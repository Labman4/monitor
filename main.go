package main

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"net"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/host"
	"github.com/sirupsen/logrus"
	"github.com/go-resty/resty/v2"
	"elpsykongroo.com/monitor/pkg/types"
	"elpsykongroo.com/monitor/pkg/vault"
	"elpsykongroo.com/monitor/pkg/s3"
)
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
			if (vault.ValidateTotp(code, *config)) {
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
			var healthData []types.HealthData
			var healthWithPrivateData []types.HealthWithPrivateData
			var isPrivate bool
			if c.GetHeader("Authorization") != "" && c.GetHeader("Authorization") != "*" {
				if isValidToken(c.GetHeader("Authorization"), *config) {
					isPrivate = true
				}
			}
			for _, item := range statuses {
				if isPrivate {
					healthWithPrivateData = append(
						healthWithPrivateData,
						types.HealthWithPrivateData{Timestamp: item[0], Status: item[1], Origin: item[2]})
				} else {
					healthData = append(healthData, types.HealthData{Timestamp: item[0], Status: item[1]})
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

	if config.EnableIpCheck {
		go reportIp(*config)
	}
	select {}
}

func readConfigFile(filePath string) (*types.Config, error) {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config types.Config
	err = json.Unmarshal(fileContent, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func getDeviceId() string {
	info, err := host.Info()
	if err !=nil {
		return ""
	}
	return info.HostID
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

func isValidToken(token string, config types.Config) bool {
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

func sync(deviceId string, config types.Config) {
	logger.Info("sync option force:", config.ForceSync)
	logger.Info("sync option duration:", config.SyncDuration)
	for range time.Tick(time.Duration(config.SyncDuration) * time.Minute) {
		//check s3
		client := s3.InitS3(config.Endpoint, config.Bucket, config.Region)
		basics := s3.BucketBasics{client}
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
				basics.Download(config.Bucket, *item.Key, dataRemotePath + *item.Key, config.ForceSync)
			}
		}
	}
}

func readCSV(c *gin.Context, deviceId string, config types.Config) [][]string {
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
	client := s3.InitS3(config.Endpoint, config.Bucket, config.Region)
	basics := s3.BucketBasics{client}
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

func reportIp(config types.Config) {
	logger.Info("start report instance ip:", config.IpCheckUrl)
	vault.ReportIpByCheck(config)
	for range time.Tick(time.Duration(config.ReportDuration) * time.Hour) {
		vault.ReportIpByCheck(config)
	}
}

func checkAPIHealth(deviceId string, config types.Config) {
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

func scheduleUploadStatus(filePath string, deviceId string, config types.Config) {
	uploadStatus(filePath, deviceId, config.Endpoint, config.Bucket, config.Region)
	for range time.Tick(time.Duration(config.UploadDuration) * time.Minute) {
		uploadStatus(filePath, deviceId, config.Endpoint, config.Bucket, config.Region)
	}
}

func uploadStatus (filePath string, deviceId string, endpoint string, bucket string, region string) {
	client := s3.InitS3(endpoint, bucket, region)
	basics := s3.BucketBasics{client}
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
