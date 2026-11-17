package config

import (
	"context"
	"errors"
	"os"

	"github.com/TechAlkurn/core/cons"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3v2 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/minio/minio-go/v7"
	mc "github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	AwsS3       *s3v2.Client
	AwsRegion   string
	IsAwsS3     bool
	MinioClient *minio.Client
)

// InitAws initializes the AWS SDK v2 and MinIO client
func InitAws() {
	IsAwsS3 = true

	AwsRegion = os.Getenv("S3_AWS_REGION")
	if AwsRegion == "" {
		AwsRegion = "us-east-1" // Default fallback
	}

	key := os.Getenv("S3_ACCESS_KEY")
	secret := os.Getenv("S3_SECRET_ACCESS_KEY")
	endpoint := os.Getenv("S3_ENDPOINT")
	s3Domain := os.Getenv("S3_DOMAIN_ENDPOINT")
	bucketName := os.Getenv("S3_BUCKET")

	// Validate required environment variables
	requiredEnv := map[string]string{
		"S3_ACCESS_KEY":        key,
		"S3_SECRET_ACCESS_KEY": secret,
		"S3_ENDPOINT":          endpoint,
		"S3_DOMAIN_ENDPOINT":   s3Domain,
		"S3_BUCKET":            bucketName,
	}
	for k, v := range requiredEnv {
		if v == "" {
			Log.Fatalf("%s is required", k)
			panic(errors.New(k + cons.IsRequiredSuffix))
		}
	}

	// Load AWS config with static credentials and custom endpoint
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(AwsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, "")),
		config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               endpoint,
					SigningRegion:     AwsRegion,
					HostnameImmutable: true,
				}, nil
			}),
		),
	)
	if err != nil {
		Log.Fatal("Failed to load AWS config: ", err)
		panic(err)
	}

	AwsS3 = s3v2.NewFromConfig(cfg)

	// Initialize MinIO client
	minioClient, err := minio.New(s3Domain, &minio.Options{
		Creds:  mc.NewStaticV4(key, secret, ""),
		Secure: true,
	})
	if err != nil {
		Log.Fatal("Failed to initialize MinIO client: ", err)
		panic(err)
	}

	MinioClient = minioClient
}
