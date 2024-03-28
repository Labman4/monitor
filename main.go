package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"net"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"
	"gopkg.in/resty.v1"
)

type HealthData struct {
	Timestamp string `json:"x"`
	Status string `json:"y"`
}

type HealthWithPrivateData struct {
	Timestamp string `json:"x"`
	Status string `json:"y"`
	Origin string `json:"origin"`
}

type BucketBasics struct {
	S3Client *s3.Client
}

type Config struct {
	Bucket     string `json:"bucket"`
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Name       string `json:"name"`
	MonitorUrl string `json:"monitorUrl"`
	ClientId   string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	IntrospectUrl string `json:"introspectUrl"`
	EnableCheck bool `json:"enableCheck"`
	EnableQuery bool `json:"enableQuery"`
	EnableUpload bool `json:"enableUpload"`
	EnableSync bool `json:"enableSync"`
	EnableWol bool `json:"enableWol"`
	forceSync bool `json:"forceSync"`
	CheckDuration int `json:"checkDuration"`
	UploadDuration int `json:"uploadDuration"`
	SyncDuration int `json:"syncDuration"`
	Password string `json:"password"`
	Username string `json:"username"`
	VaultUri string `json:"vaultUri"`

}

type Introspect struct {
	Active bool `json:"active"`
}

var logger = logrus.New()

func main() {
	deviceId := getDeviceId()
	homePath, err := os.UserHomeDir()
	if err != nil {
		logger.Error("get home path errpr")
		return
	}
	var configFilePath string 
	operateSystem := runtime.GOOS;
	if operateSystem == "windows" {
		configFilePath =  homePath + `\.aws\` + `config.json`
	} else {
		configFilePath = homePath + `/.aws/` + `config.json`
	}

	config, err := readConfigFile(configFilePath)
	if err != nil {
		logger.Error("Error reading config file:", err)
		return
	}
	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	})
	if (config.EnableWol) {
		r.POST("/wol", func(c *gin.Context) {
			if c != nil {
				parseErr := c.Request.ParseForm()
				if parseErr != nil {
					c.String(http.StatusBadRequest, "Failed to parse form data")
					return 
				}
			}
			mac := c.Request.Form.Get("mac")
			code := c.Request.Form.Get("code")
			logger.Info("mac:", mac)
			logger.Info("code:", code)
			if (validateTotp(code, *config)) {
				err := wakeOnLAN(mac)
				logger.Error("wol err:", err)
			} else {
				logger.Warn("code mismatch")
			}			
		})
	}
	
	r.GET("/", func(c *gin.Context) {
		clientIP := c.ClientIP()
		c.String(http.StatusOK, clientIP)
	})

	r.GET("/ip", func(c *gin.Context) {
		clientIP := c.ClientIP()
		c.String(http.StatusOK, clientIP)
	})

	r.PUT("/status", func(c *gin.Context) {
		// if c.GetHeader("Authorization") != "" {
		// 	if isValidToken(c.GetHeader("Authorization"), *config) {
				writeCSV(c, deviceId, nil, config.Name)
		// 	}
		// }
	})

	if (config.EnableQuery) {
		r.GET("/status", func(c *gin.Context) {
			statuses := readCSV(c, deviceId, *config)
			var healthData []HealthData
			var healthWithPrivateData []HealthWithPrivateData
			var isPrivate bool
			if c.GetHeader("Authorization") != "" {
				if isValidToken(c.GetHeader("Authorization"), *config) {
					isPrivate = true
				}
			}
			for _, item := range statuses {
				if isPrivate {
					healthWithPrivateData = append(
						healthWithPrivateData,
						HealthWithPrivateData{Timestamp: item[0], Status: item[1], Origin: item[2]})
				} else {
					healthData = append(healthData, HealthData{Timestamp: item[0], Status: item[1]})
				}
			}
			if isPrivate {
				c.JSON(http.StatusOK, healthWithPrivateData)
			} else {
				c.JSON(http.StatusOK, healthData)
			}
		})
	}

	go func() {
		if err := r.Run(":11415"); err != nil {
			logger.Error("Error starting server:", err)
		}
	}()
	
	if config.EnableCheck {
		go checkAPIHealth(deviceId, *config)
	}

	if config.EnableUpload {
		go scheduleUploadStatus(generateDatapath(config.Name), deviceId, *config)
	}

	if config.EnableSync {
		go sync(deviceId, *config)
	}

	select {}
}

func getDeviceId() string {
	info, err := host.Info()
	if err !=nil {
		return ""
	}
	return info.HostID
}

func isValidToken(token string, config Config) bool {
	client := resty.New()

	resp, err := client.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBasicAuth(config.ClientId, config.ClientSecret).
		SetFormData(map[string]string{
			"token": token,
		}).
		Post(config.IntrospectUrl)

	if err != nil {
		logger.Error("introspect err:", err)
		return false
	}
	var result map[string]interface{}
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		return false
	}
	if result["Active"] != nil {
		active := result["Active"].(bool)
		return active
	}
	return false
}

func readConfigFile(filePath string) (*Config, error) {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = json.Unmarshal(fileContent, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func sync(deviceId string, config Config) {
	logger.Info("sync option force:", config.forceSync)
	logger.Info("sync option duration:", config.SyncDuration)
	for range time.Tick(time.Duration(config.SyncDuration) * time.Minute) {
		//check s3
		client := initS3(config.Endpoint, config.Bucket, config.Region)
		basics := BucketBasics{client}
		bucketExist,err := basics.BucketExists(config.Bucket)
		if err != nil {
			logger.Error("BucketExists error:", err)
		}
		if !bucketExist {
			basics.CreateBucket(config.Bucket, config.Region)
		}
		//
		listObjects, err := basics.ListObjects(config.Bucket);
		if err != nil {
			logger.Error("listObject error:", err)
		}
		dataRemotePath := generateRemoteDatapath(config.Name)

		//sync with s3
		for _, item := range listObjects {
			if isDate(strings.Split(*item.Key, "_")[0]) {
				basics.Download(config.Bucket, *item.Key, dataRemotePath + *item.Key, config.forceSync)
			}
		}
	}
}

func readCSV(c *gin.Context, deviceId string, config Config) [][]string {
	//handle params
	limit := c.Query("limit")
	date := c.Query("date")
	var limitInt int = 0
	var checkFlag bool = false
	if limit != "" {
		limitParseInt, err := strconv.Atoi(limit);
		if err != nil {
			logger.Error("parse int err")
			return nil
		}
		limitInt = limitParseInt
		if limitInt <= 0 {
			logger.Warn("ilegal input, limit:", limit)
			return nil
		}
	}
	if limitInt == 1 {
		checkFlag = true
	}
	//calucate date need fetch
	var dates []string
	dataPath := generateDatapath(config.Name)
	dataRemotePath := generateRemoteDatapath(config.Name)
	currentDate := time.Now()
	formatData := currentDate.Format("2006-01-02");
	if date != "" {
		if !isDate(date) {
			return nil
		} else {
			dates = append(dates, date)
			checkFlag = true
		}
	} else {	
		dates = append(dates, formatData)
		for i := 0 ; i < limitInt - 1; i++ {
			currentDate = currentDate.AddDate(0, 0, -1)
			formatData := currentDate.Format("2006-01-02");
			dates = append(dates, formatData)
		}
	}

	logger.Info("will fetch data:", dates)

	//check s3
	client := initS3(config.Endpoint, config.Bucket, config.Region)
	basics := BucketBasics{client}
	bucketExist,err := basics.BucketExists(config.Bucket)
	if err != nil {
		logger.Error("BucketExists error:", err)
		return nil
	}
	if !bucketExist {
		basics.CreateBucket(config.Bucket, config.Region)
	}
	//start read s3 data
	logger.Info("list remote dir", dataRemotePath)
	logger.Info("forceCheck: ", checkFlag)
	files, err := os.ReadDir(dataRemotePath)
	if err != nil {
		return nil
	}
	var fileNames []string
	for _, file := range files {
		if !file.IsDir() && isDate(strings.Split(file.Name(), "_")[0]) {
			// remove local file not exist in s3
			err := basics.Download(config.Bucket, file.Name(), dataRemotePath + file.Name(), false)
			if err == nil {
				fileDate := strings.Split(file.Name(), "_")[0];
				if len(dates) > 0 {
					flag := false
					for _,d := range dates {	
						if strings.Split(file.Name(), "_")[0] == d {
							flag = true
							if (checkFlag) {
								basics.Download(config.Bucket, file.Name(), dataRemotePath + file.Name(), checkFlag)
							}
						}
					};
					if flag {
						fileNames = append(fileNames, file.Name())
					}
				} else if date != "" && date == fileDate {
					fileNames = append(fileNames, file.Name())
				}
			}
		}
	}
	var statuses [][]string
	if len(fileNames) > 0 {
		logger.Info("start read local data from remote:", fileNames[0] + "----" + fileNames[len(fileNames) - 1])
	}
	for i := 0; i < len(fileNames); i++ {		
		file, err := os.Open(dataRemotePath + fileNames[i])
		if err != nil {
			logger.Error("read error:", fileNames[i])
			return nil
		}
		defer file.Close()
		reader := csv.NewReader(file)
		status, err := reader.ReadAll()
		statuses = append(statuses, status...)
		if err != nil {
			logger.Error("read err")
			return nil
		}		
	}
	if (date == "" || date == formatData) {
		stautsData, err := readSingleFile(dataPath + formatData);
		if err != nil {
			logger.Error("read today data err:", err)
		}
		statuses = append(statuses, stautsData...)
	}
	sort.Slice(statuses, func(i, j int) bool {
		timei, _ := time.Parse("2006-01-02 15:04:05 -0700", statuses[i][0])
		timej, _ := time.Parse("2006-01-02 15:04:05 -0700", statuses[j][0])
		return timei.Before(timej)
	})		
	return statuses
}

func readSingleFile (filename string) ([][] string, error) {
	_,err := os.Stat(filename)
	if err == nil {
		file, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader := csv.NewReader(file)
		status, err := reader.ReadAll()
		if err != nil {
			return nil, err
		}	
		return status, nil
	} else {
		return nil, err	
	}
}

func writeCSV(c *gin.Context, deviceId string, dataMap map[string][]string, name string) {
	dataPath := generateDatapath(name)
	currentDate := time.Now()
	formatData := currentDate.Format("2006-01-02");
	if (dataPath != "") {
		file, err := os.OpenFile(dataPath + formatData, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
  
		if err != nil {
			return
		}
		defer file.Close()
	
		writer := csv.NewWriter(file)
		defer writer.Flush()
		var data map[string][]string
		var origin string
		if c != nil {
			origin = c.ClientIP()
			parseErr := c.Request.ParseForm()
			if parseErr != nil {
				c.String(http.StatusBadRequest, "Failed to parse form data")
				return 
			}
			data = c.Request.Form
		} else {
			origin = name + "_" + deviceId
			data = dataMap
		}
		for key, values := range data {
			for _, value := range values {
				err := writer.Write([]string{key, value, origin})
				if err != nil {
					c.String(http.StatusInternalServerError, "Failed to write to CSV file")
					return 
				}
			}
		}
	}
}

func generateDatapath (name string) string {
	operateSystem := runtime.GOOS;
	filename := "/var/log/" + name + "/";
	if operateSystem != "linux" {
		homePath, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		if operateSystem == "windows" {
			filename = homePath + `\`+ name + `\`
		} else {
			filename = homePath + "/" + name+ "/"
		}
	}
	fileInfo, err := os.Stat(filename)
    if err != nil {
        if os.IsNotExist(err) {
			logger.Error("dir not exist")
			err := os.Mkdir(filename, 0755)
			if err != nil {
				logger.Error("Error:", err)
				return ""
			}
			return filename
		} else {
            logger.Error("other err:", err)
        }
        return ""
    }

	if !fileInfo.Mode().IsDir() {
        logger.Error("not dir")
    }
	return filename
}

func generateRemoteDatapath (name string) string {
	operateSystem := runtime.GOOS;
	filename := "/var/log/" + name + "/remote/";
	if operateSystem != "linux" {
		homePath, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		if operateSystem == "windows" {
			filename = homePath + `\`+ name + `\remote\` 
		} else {
			filename = homePath + "/" + name + "/remote/" 
		}
	}
	fileInfo, err := os.Stat(filename)
    if err != nil {
        if os.IsNotExist(err) {
			logger.Error("dir not exist")
			err := os.Mkdir(filename, 0755)
			if err != nil {
				logger.Error("Error:", err)
				return ""
			}
			return filename
		} else {
            logger.Error("other err:", err)
        }
        return ""
    }

	if !fileInfo.Mode().IsDir() {
        logger.Error("not dir")
    }
	return filename
}

func isDate(str string) bool {
	dateLayout := "2006-01-02" 
	_, err := time.Parse(dateLayout, str)
	return err == nil
}

func checkAPIHealth(deviceId string, config Config) {
	logger.Info("start check health with:", config.MonitorUrl)
	for range time.Tick(time.Duration(config.CheckDuration) * time.Second) {
		resp, err := http.Get(config.MonitorUrl)
		currentTime := time.Now()
		currentTimeString := currentTime.Format("2006-01-02 15:04:05 -0700")
		healthMap := make(map[string][]string)
		if err != nil {
			logger.Error("Error checking API health:", err)
			healthMap[currentTimeString] = []string{"500"}
			writeCSV(nil, deviceId, healthMap, config.Name)
		} else {
			if resp.StatusCode != http.StatusOK {
				logger.Info("API is unhealthy! Status code:", resp.StatusCode)
				satusCodeStr := strconv.Itoa(resp.StatusCode)
				healthMap[currentTimeString] = []string{satusCodeStr}
				writeCSV(nil, deviceId, healthMap, config.Name)
			} 
			resp.Body.Close()
		}
	}
}

func scheduleUploadStatus(filePath string, deviceId string, config Config) {
	uploadStatus(filePath, deviceId, config.Endpoint, config.Bucket, config.Region)
	for range time.Tick(time.Duration(config.UploadDuration) * time.Minute) {
		uploadStatus(filePath, deviceId, config.Endpoint, config.Bucket, config.Region)
	}
}


func uploadStatus (filePath string, deviceId string, endpoint string, bucket string, region string) {
	client := initS3(endpoint, bucket, region)
	basics := BucketBasics{client}
	currentTime := time.Now()
	formatData := currentTime.Format("2006-01-02");
	logger.Info("list local dir:", filePath)
	files, err := os.ReadDir(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Error("start create dir:", filePath)
			os.Create(filePath)
		} else {
			logger.Error("read all error:", err)
		}
	}
	for _, file := range files {
		if !file.IsDir() && isDate(file.Name()) {
			err := basics.Upload(bucket, formatData + "_" + deviceId, filePath + file.Name())
			if err != nil {
				continue
			}
			if (file.Name() != formatData) {
				os.Remove(filePath + file.Name())
			}
		}
	}
}

func initS3 (endpoint string, bucket string, region string) *s3.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		logger.Error("init s3:", err)
	}
	client := s3.NewFromConfig(cfg, func (o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
    })
	return client
}

func (basics BucketBasics) BucketExists(bucketName string) (bool, error) {
	_, err := basics.S3Client.HeadBucket(context.TODO(), &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	exists := true
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) {
			switch apiError.(type) {
			case *types.NotFound:
				logger.Error("Bucket is available", bucketName)
				exists = false
				err = nil
			default:
				log.Printf("Either you don't have access to bucket %v or another error occurred. "+
					"Here's what happened: %v\n", bucketName, err)
			}
		}
	} else {
		log.Printf("Bucket %v exists and you already own it.", bucketName)
	}

	return exists, err
}

func (basics BucketBasics) CreateBucket(name string, region string) error {
	_, err := basics.S3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
		Bucket: aws.String(name),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		},
	})
	if err != nil {
		log.Printf("Couldn't create bucket %v in Region %v. Here's why: %v\n",
			name, region, err)
	}
	return err
}

func (basics BucketBasics) Upload(bucketName string, objectKey string, fileName string) error {
	_, err := os.Stat(fileName)
	if err == nil {
		logger.Info("file exist, start sync data between local with remote:", fileName)
		headResult, err := basics.HeadObject(bucketName, objectKey)
		if err != nil {
			var bne *types.NotFound
			if errors.As(err, &bne) {
				logger.Info("remote data not exist, start upload:", fileName)
				err := basics.UploadFile(bucketName, objectKey, fileName)
				logger.Info("remote data not exist, end upload:", fileName)	
				if err != nil {
					return err
				}
				return nil
			} else {
				return err
			}
		} 
	    if !checkFileBetweenRemoteAndLocal(headResult, fileName) {
			logger.Info("check failed, start upload local data to remote:", fileName)
			err :=basics.UploadFile(bucketName, objectKey, fileName)
			logger.Info("check failed, end upload local data to remote:", fileName)
			if err != nil {
				return err
			}
		}
	} else if os.IsNotExist(err) {
		logger.Info("local data not exist, skip upload:", fileName)	
	} else {
		logger.Error("Error checking file existence:", err)
	}
	return err
}

func checkFileBetweenRemoteAndLocal (headResult *s3.HeadObjectOutput, fileName string) bool {
	local256, err := calculateSHA256(fileName)
	if err != nil {
		logger.Error("sha256 err:", err)
	}
	if headResult.ChecksumSHA256 != nil && *headResult.ChecksumSHA256 != ""  {
		logger.Debug("check sha256 remote:", headResult.ChecksumSHA256)	
		logger.Debug("check sha256 local:", local256)
		if headResult.ChecksumSHA256 != &local256 {
			return false
		}
	} else if headResult.Metadata["x-amz-meta-sha256"] != "" {
		logger.Debug("check metadata remote:", headResult.Metadata["x-amz-meta-sha256"])
		logger.Debug("check metadata local:", local256)
		if headResult.Metadata["x-amz-meta-sha256"] != local256 {
			return false
		}
	} else {
		logger.Info("not enough data to check, just do it:", fileName)
		return false
	}
	logger.Info("check success, skip operation")
	return true
}

func (basics BucketBasics) UploadFile(bucketName string, objectKey string, fileName string) error {
	logger.Info("start upload data:", fileName)
	sha256, err := calculateSHA256(fileName)
	if err != nil {
		logger.Error("sha256 err:", err)
	}
	file, err := os.Open(fileName)
	if err != nil {
		logger.Error("Couldn't open file", err)
	} else {
		defer file.Close()
		_, err = basics.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
			Body:   file,
			Metadata: map[string]string{
				"x-amz-meta-sha256": sha256,
			},
		})
		if err != nil {
			logger.Error("Couldn't upload file", err)
		}
		logger.Info("end upload data:", fileName)
	}
	return err
}

func (basics BucketBasics) Download(bucketName string, objectKey string, fileName string, checkflag bool) error {
	_, err := os.Stat(fileName)
	if err == nil {
		// only check custom date and today 
		if (checkflag) {
			logger.Info("file exist, start sync data between local with remote:", fileName)
			headResult, err := basics.HeadObject(bucketName, objectKey)
			if err != nil {
				var bne *types.NotFound
				if errors.As(err, &bne) {
					logger.Info("remote data not exist, skip download, will remove local data to sync:", fileName)
					os.Remove(fileName)
					return err
				}
				return nil	
			} 
			if !checkFileBetweenRemoteAndLocal(headResult, fileName) {
				logger.Info("check failed, start fetch remote data to local:", fileName)
				basics.DownloadFile(bucketName, objectKey, fileName)
				logger.Info("check failed, end fetch remote data to local:", fileName)	
			}
		}
	} else if os.IsNotExist(err) {
		basics.DownloadFile(bucketName, objectKey, fileName)
	} else {
		logger.Error("Error checking file existence:", err)
	}
	return nil
}

func (basics BucketBasics) DownloadFile(bucketName string, objectKey string, fileName string) error {
	logger.Info("start download data:", fileName)
	result, err := basics.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		log.Printf("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		return err
	}
	defer result.Body.Close()
	logger.Info("end download data:", fileName)
	file, err := os.Create(fileName)
	if err != nil {
		log.Printf("Couldn't create file %v. Here's why: %v\n", fileName, err)
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("Couldn't read object body from %v. Here's why: %v\n", objectKey, err)
	}
	_, err = file.Write(body)
	return err
}

func (basics BucketBasics) ListObjects(bucketName string) ([]types.Object, error) {
	result, err := basics.S3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	var contents []types.Object
	if err != nil {
		log.Printf("Couldn't list objects in bucket %v. Here's why: %v\n", bucketName, err)
	} else {
		contents = result.Contents
	}
	return contents, err
}

func (basics BucketBasics) HeadObject(bucketName string, key string) (*s3.HeadObjectOutput, error) {
	result, err := basics.S3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key: aws.String(key),
	})
	if err != nil {
		logger.Error("head objects err:", err.Error())
	}
	return result, err
}

func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	hashInBytes := hash.Sum(nil)
	hashString := hex.EncodeToString(hashInBytes)

	return hashString, nil
}

func validateTotp (code string, config Config) bool{
	client := resty.New()
	body := map[string]string{
		"password": config.Password,
	}
	loginResp, err := client.R().
	SetHeader("Content-Type", "application/json").
	SetBody(body).Post(config.VaultUri + "/v1/auth/userpass/login/" + config.Username)
	if err != nil {
		logger.Error("login vault err:", err)
		return false
	}
	logger.Info("login vault resp:", loginResp.String())
	var data map[string]interface{}
    err = json.Unmarshal(loginResp.Body(), &data)
    if err != nil {
        logger.Error("Error decoding JSON:", err)
        return false
    }

	token :=  data["auth"].(map[string]interface{})["client_token"].(string)
	logger.Info("vault token:", token)

	resp, err := client.R().
		SetHeader("X-Vault-Token", token).
		Get(config.VaultUri + "/v1/totp/code/" + config.Username)
		logger.Info("vault token:", resp.String())

	if err != nil {
		logger.Error("get code err:", err)
		return false
	}
	var codeData map[string]interface{}
	err = json.Unmarshal(resp.Body(), &codeData)
	if err != nil {
		return false
	}
	c :=  codeData["data"].(map[string]interface{})["code"].(string)
	if (code == c) {
		return true
	} else {
		return false
	}
}

func wakeOnLAN(macAddr string) error {
    interfaces, err := net.Interfaces()
	if err != nil {
        return err
    }
	logger.Info("list interfaces:", interfaces)
	mac, err := net.ParseMAC(macAddr)
	broadcastAddr := net.IPv4(255, 255, 255, 255)
	udpAddr := &net.UDPAddr{
		IP:   broadcastAddr,
		Port: 9,
	}
    if err != nil {
        return err
    }
	magicPacket := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	for i := 0; i < 16; i++ {
		magicPacket = append(magicPacket, mac...)
	}

    for _, iface := range interfaces {
        if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
            continue
        }

		addrs, err := iface.Addrs()
		if err != nil {
			return err
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				ipv4Addr := ipnet.IP.To4()
				logger.Info("bind addr:", ipv4Addr)
				conn, err := net.DialUDP("udp", &net.UDPAddr{IP: ipv4Addr, Port: 0}, udpAddr)
				if err != nil {
					return err
				}
				defer conn.Close()
			
				_, err = conn.Write(magicPacket)
				if err != nil {
					return err
				}
			}		
		}
    }
    return nil
}
