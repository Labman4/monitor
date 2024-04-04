package types

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type BucketBasics struct {
	S3Client *s3.Client
}

type HealthData struct {
	Timestamp string `json:"x"`
	Status string `json:"y"`
}

type HealthWithPrivateData struct {
	Timestamp string `json:"x"`
	Status string `json:"y"`
	Origin string `json:"origin"`
}

type Config struct {
	Bucket     string `json:"bucket"`
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Name       string `json:"name"`
	MonitorUrl string `json:"monitorUrl"`
	IpCheckUrl string `json:"ipCheckUrl"`
	ClientId   string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	IntrospectUrl string `json:"introspectUrl"`
	EnableCheck bool `json:"enableCheck"`
	EnableIpCheck bool `json:"enableIpCheck"`
	EnableQuery bool `json:"enableQuery"`
	EnableUpload bool `json:"enableUpload"`
	EnableSync bool `json:"enableSync"`
	EnableWol bool `json:"enableWol"`
	ForceSync bool `json:"forceSync"`
	CheckDuration int `json:"checkDuration"`
	UploadDuration int `json:"uploadDuration"`
	SyncDuration int `json:"syncDuration"`
	ReportDuration int `json:"reportDuration"`
	Password string `json:"password"`
	Username string `json:"username"`
	VaultPublicUser string `json:"vaultPublicUser"`
	VaultUri string `json:"vaultUri"`
	VaultCloudUri string `json:"vaultCloudUri"`
    VaultConfigPath string `json:"vaultConfigPath"`
	VaultCustomKey string `json:"vaultCustomKey"`
}

type Introspect struct {
	Active bool `json:"active"`
}