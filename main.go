package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
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
	CheckDuration int `json:"checkDuration"`
	UploadDuration int `json:"uploadDuration"`
}

type Introspect struct {
	Active bool `json:"active"`
}

var logger = logrus.New()

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
	var result Introspect
	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		return false
	}
	return result.Active
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

func main() {
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
		fmt.Println("Error reading config file:", err)
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

	r.GET("/", func(c *gin.Context) {
		clientIP := c.ClientIP()
		c.String(http.StatusOK, clientIP)
	})

	r.PUT("/status", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" {
			if isValidToken(c.GetHeader("Authorization"), *config) {
				writeCSV(c, nil, config.Name)
			}
		}
	})

	r.GET("/status", func(c *gin.Context) {
		statuses := readCSV(c, *config)
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

	go func() {
		if err := r.Run(":11415"); err != nil {
			fmt.Println("Error starting server:", err)
		}
	}()
	
	if config.EnableCheck {
		go checkAPIHealth(*config)
	}

	go scheduleUploadStatus(generateDatapath(config.Name), *config)

	select {}
}

func readCSV(c *gin.Context, config Config) [][]string {
	limit := c.Query("limit")
	date := c.Query("date")
	limitInt, err := strconv.Atoi(limit);
	if err != nil {
		logger.Error("parse int err")
		return nil
	}
	if limitInt <= 0 {
		logger.Warn("ilegal input, limit:", limit)
	    return nil
	}
	dataPath := generateDatapath(config.Name)
	dataRemotePath := generateRemoteDatapath(config.Name)
	currentDate := time.Now()
	formatData := currentDate.Format("2006-01-02");
	if limitInt == 1 || date == formatData {
		logger.Info("read from local")
		singleData,err := readSingleFile(dataPath + date)
		if err != nil{
			return nil
		} 
		return singleData
	} else {
		client := initS3(config.Endpoint, config.Bucket, config.Region)
		basics := BucketBasics{client}
		bucketExist,err := basics.BucketExists(config.Bucket)
		if !bucketExist {
			basics.CreateBucket(config.Bucket, config.Region)
		}
		if (date != "") {
			logger.Info("fetch data date:", date)
			basics.Download(config.Bucket, date, dataRemotePath + date)
		} else {
			logger.Info("fetch data limit:", limit)
			if err != nil {
				logger.Error("read error:", err)
				return nil
			}
			listObjects, err := basics.ListObjects(config.Bucket);
			if err != nil {
				logger.Error("listObject error:", err)
				return nil
			}
			for _, item := range listObjects {
				if isDate(*item.Key) {
					basics.Download(config.Bucket, *item.Key, dataRemotePath + *item.Key)
				}
			}
			logger.Info("list remote dir:", dataRemotePath)
			files, err := os.ReadDir(dataRemotePath)
			if err != nil {
				logger.Error("read all error:", err)
				return nil
			}
			var fileNames []string
			for _, file := range files {
				if !file.IsDir() && isDate(file.Name()) {
					fileNames = append(fileNames, file.Name())
				}
			}
			if (len(fileNames) > 1) {
				sort.Slice(fileNames, func(i, j int) bool {
					timei, _ := time.Parse("2006-01-02", fileNames[i])
					timej, _ := time.Parse("2006-01-02", fileNames[j])
					return timei.Before(timej)
				})
			}
			var statuses [][]string
			logger.Info("start read remote data:", fileNames[0] + "-" + fileNames[len(fileNames) - 1])
			for i := 0; i < limitInt && i < len(fileNames); i++ {
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
					fmt.Println("read err")
					return nil
				}		
			}
			stautsData, err := readSingleFile(dataPath + formatData);
			if err != nil {
				return nil
			}
			statuses = append(statuses, stautsData...)

			return statuses
		}  
		logger.Info("other status")
		return nil		
	}
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

func writeCSV(c *gin.Context, dataMap map[string][]string, name string) {
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
			origin = name
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
		currentUser, err := user.Current()
		if err != nil {
			fmt.Println("Error:", err)
			return ""
		}
		filename = currentUser.HomeDir
		if operateSystem == "windows" {
			filename = filename + `\`+ name + `\`
		} else {
			filename = filename + "/" + name+ "/"
		}
	}
	fileInfo, err := os.Stat(filename)
    if err != nil {
        if os.IsNotExist(err) {
			fmt.Println("dir not exist")
			err := os.Mkdir(filename, 0755)
			if err != nil {
				fmt.Println("Error:", err)
				return ""
			}
		} else {
            fmt.Println("other err:", err)
        }
        return ""
    }

	if !fileInfo.Mode().IsDir() {
        fmt.Println("not dir")
    }
	return filename
}

func generateRemoteDatapath (name string) string {
	operateSystem := runtime.GOOS;
	filename := "/var/log/" + name + "/remote/";
	if operateSystem != "linux" {
		currentUser, err := user.Current()
		if err != nil {
			fmt.Println("Error:", err)
			return ""
		}
		filename = currentUser.HomeDir
		if operateSystem == "windows" {
			filename = filename + `\`+ name + `\remote\` 
		} else {
			filename = filename + "/" + name + "/remote/" 
		}
	}
	fileInfo, err := os.Stat(filename)
    if err != nil {
        if os.IsNotExist(err) {
			fmt.Println("dir not exist")
			err := os.Mkdir(filename, 0755)
			if err != nil {
				fmt.Println("Error:", err)
				return ""
			}
		} else {
            fmt.Println("other err:", err)
        }
        return ""
    }

	if !fileInfo.Mode().IsDir() {
        fmt.Println("not dir")
    }
	return filename
}

func isDate(str string) bool {
	dateLayout := "2006-01-02" 
	_, err := time.Parse(dateLayout, str)
	return err == nil
}

func checkAPIHealth(config Config) {
	for range time.Tick(time.Duration(config.CheckDuration) * time.Second) {
		currentTime := time.Now()
		resp, err := http.Get(config.MonitorUrl)
		currentTimeString := currentTime.Format("2006-01-02 15:04:05")
		healthMap := make(map[string][]string)
		if err != nil {
			fmt.Printf("Error checking API health: %v\n", err)
			healthMap[currentTimeString] = []string{"500"}
			writeCSV(nil, healthMap, config.Name)
		} else {
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("API is unhealthy! Status code: %d\n", resp.StatusCode)
				satusCodeStr := strconv.Itoa(resp.StatusCode)
				healthMap[currentTimeString] = []string{satusCodeStr}
				writeCSV(nil, healthMap, config.Name)
			} 
			resp.Body.Close()
		}
	}
}

func scheduleUploadStatus(filePath string, config Config) {
	uploadStatus(filePath, config.Endpoint, config.Bucket, config.Region)
	for range time.Tick(time.Duration(config.UploadDuration) * time.Hour) {
		uploadStatus(filePath, config.Endpoint, config.Bucket, config.Region)
	}
}

func uploadStatus (filePath string, endpoint string, bucket string, region string) {
	client := initS3(endpoint, bucket, region)
	basics := BucketBasics{client}
	currentTime := time.Now()
	currentDate :=currentTime.AddDate(0, 0, -1)
	formatData := currentDate.Format("2006-01-02");
	basics.Upload(bucket, formatData, filePath + formatData)
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

func (basics BucketBasics) Upload(bucketName string, objectKey string, fileName string) {
	_, err := os.Stat(fileName)
	if err == nil {
		logger.Info("file exist, start sync data between local with remote:", fileName)
		headResult, err := basics.HeadObject(bucketName, objectKey)
		if err != nil {
			var bne *types.NoSuchKey
			if errors.As(err, &bne) {	
				logger.Info("remote data not exist, start upload:", fileName)	
				basics.UploadFile(bucketName, objectKey, fileName)
				logger.Info("remote data not exist, end upload:", fileName)	
			} 
		}
		if !checkFileBetweenRemoteAndLocal(headResult, fileName) {
			logger.Info("check failed, start upload local data to remote:", fileName)
			basics.UploadFile(bucketName, objectKey, fileName)
			logger.Info("check failed, end upload local data to remote:", fileName)
		}
	} else if os.IsNotExist(err) {
		logger.Info("local data not exist, skip upload:", fileName)	
	} else {
		logger.Error("Error checking file existence:", err)
	}
}

func checkFileBetweenRemoteAndLocal (headResult *s3.HeadObjectOutput, fileName string) bool {
	local256, err := calculateSHA256(fileName)
	if err != nil {
		logger.Error("sha256 err:", err)
	}
	if headResult.ChecksumSHA256 != nil && *headResult.ChecksumSHA256 != ""  {
		logger.Info("check sha256 remote:", headResult.ChecksumSHA256)	
		logger.Info("check sha256 local:", local256)
		if headResult.ChecksumSHA256 != &local256 {
			return false
		}
	} else if headResult.Metadata["x-amz-meta-sha256"] != "" {
		logger.Info("check metadata remote:", headResult.Metadata["x-amz-meta-sha256"])
		logger.Info("check metadata local:", local256)
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

func (basics BucketBasics) Download(bucketName string, objectKey string, fileName string) {
	_, err := os.Stat(fileName)
	if err == nil {
		logger.Info("file exist, start sync data between local with remote:", fileName)
		headResult, err := basics.HeadObject(bucketName, objectKey)
		if err != nil {
			var bne *types.NoSuchKey
			if errors.As(err, &bne) {
				logger.Info("remote data not exist, skip download:", fileName)	
			} 
			return
		}
		if !checkFileBetweenRemoteAndLocal(headResult, fileName) {
			logger.Info("check failed, start fetch remote data to local:", fileName)
			basics.DownloadFile(bucketName, objectKey, fileName)
			logger.Info("check failed, end fetch remote data to local:", fileName)	
		}
	} else if os.IsNotExist(err) {
		basics.DownloadFile(bucketName, objectKey, fileName)
	} else {
		logger.Error("Error checking file existence:", err)
	}
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
		logger.Error("head objects err:", err)
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