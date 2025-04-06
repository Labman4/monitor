package s3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/sirupsen/logrus"
	"io"
	"os"
)

var logger = logrus.New()

type BucketBasics struct {
	S3Client *s3.Client
}

func (basics BucketBasics) ListObjects(bucketName string) ([]types.Object, error) {
	result, err := basics.S3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	var contents []types.Object
	if err != nil {
		logger.Error("Couldn't list objects in bucket %v. Here's why: %v\n", bucketName, err)
	} else {
		contents = result.Contents
	}
	return contents, err
}

func (basics BucketBasics) HeadObject(bucketName string, key string) (*s3.HeadObjectOutput, error) {
	result, err := basics.S3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		logger.Error("head objects err:", err.Error())
	}
	return result, err
}

func (basics BucketBasics) DownloadFile(bucketName string, objectKey string, fileName string) error {
	logger.Debug("start download data:", fileName)
	result, err := basics.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		logger.Error("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		return err
	}
	defer result.Body.Close()
	logger.Debug("end download data:", fileName)
	file, err := os.Create(fileName)
	if err != nil {
		logger.Error("Couldn't create file %v. Here's why: %v\n", fileName, err)
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		logger.Error("Couldn't read object body from %v. Here's why: %v\n", objectKey, err)
	}
	_, err = file.Write(body)
	return err
}

func (basics BucketBasics) CreateBucket(name string, region string) error {
	_, err := basics.S3Client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
		Bucket: aws.String(name),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		},
	})
	if err != nil {
		logger.Error("Couldn't create bucket %v in Region %v. Here's why: %v\n",
			name, region, err)
	}
	return err
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
				logger.Error("Either you don't have access to bucket %v or another error occurred. "+
					"Here's what happened: %v\n", bucketName, err)
			}
		}
	} else {
		logger.Error("Bucket %v exists and you already own it.", bucketName)
	}

	return exists, err
}

func (basics BucketBasics) Upload(bucketName string, objectKey string, fileName string) error {
	_, err := os.Stat(fileName)
	if err == nil {
		logger.Info("file exist, start sync data between local with remote:", fileName)
		headResult, err := basics.HeadObject(bucketName, objectKey)
		if err != nil {
			var bne *types.NotFound
			if errors.As(err, &bne) {
				logger.Debug("remote data not exist, start upload:", fileName)
				err := basics.UploadFile(bucketName, objectKey, fileName)
				logger.Debug("remote data not exist, end upload:", fileName)
				if err != nil {
					return err
				}
				return nil
			} else {
				return err
			}
		}
		if !checkFileBetweenRemoteAndLocal(headResult, fileName) {
			logger.Debug("check failed, start upload local data to remote:", fileName)
			err := basics.UploadFile(bucketName, objectKey, fileName)
			logger.Debug("check failed, end upload local data to remote:", fileName)
			if err != nil {
				return err
			}
		}
	} else if os.IsNotExist(err) {
		logger.Debug("local data not exist, skip upload:", fileName)
	} else {
		logger.Error("Error checking file existence:", err)
	}
	return err
}

func checkFileBetweenRemoteAndLocal(headResult *s3.HeadObjectOutput, fileName string) bool {
	local256, err := calculateSHA256(fileName)
	if err != nil {
		logger.Error("sha256 err:", err)
	}
	if headResult.ChecksumSHA256 != nil && *headResult.ChecksumSHA256 != "" {
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
	logger.Debug("check success, skip operation")
	return true
}

func (basics BucketBasics) UploadFile(bucketName string, objectKey string, fileName string) error {
	logger.Debug("start upload data:", fileName)
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
		logger.Debug("end upload data:", fileName)
	}
	return err
}

func (basics BucketBasics) Download(bucketName string, objectKey string, fileName string, checkflag bool) error {
	_, err := os.Stat(fileName)
	if err == nil {
		// only check custom date and today
		if checkflag {
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
				logger.Debug("check failed, start fetch remote data to local:", fileName)
				basics.DownloadFile(bucketName, objectKey, fileName)
				logger.Debug("check failed, end fetch remote data to local:", fileName)
			}
		}
	} else if os.IsNotExist(err) {
		basics.DownloadFile(bucketName, objectKey, fileName)
	} else {
		logger.Error("Error checking file existence:", err)
	}
	return nil
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

func InitS3(endpoint string, bucket string, region string) *s3.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		logger.Error("init s3:", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	return client
}
