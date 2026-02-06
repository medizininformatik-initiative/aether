package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// S3Uploader defines the interface for uploading files to S3.
type S3Uploader interface {
	UploadFile(ctx context.Context, localPath, s3Key string) (etag string, err error)
	GetBucket() string
}

// AWSS3Uploader implements S3Uploader using the AWS SDK v2.
type AWSS3Uploader struct {
	client *s3.Client
	bucket string
	logger *lib.Logger
}

// proxyAuthTransport wraps an http.RoundTripper to inject a Proxy-Authorization
// header on every request. This allows HTTP-level auth (basic/oauth2) for proxied
// S3 endpoints without conflicting with the AWS Authorization header.
type proxyAuthTransport struct {
	base      http.RoundTripper
	authValue string
}

func (t *proxyAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Proxy-Authorization", t.authValue)
	return t.base.RoundTrip(req)
}

// NewAWSS3Uploader creates a new S3 uploader from config.
func NewAWSS3Uploader(s3Config models.S3Config, auth models.AuthConfig, logger *lib.Logger) (*AWSS3Uploader, error) {
	httpClient := &http.Client{}

	// Wrap HTTP client with proxy auth if configured
	if auth.Username != "" && auth.Password != "" {
		creds := auth.Username + ":" + auth.Password
		encoded := base64.StdEncoding.EncodeToString([]byte(creds))
		httpClient.Transport = &proxyAuthTransport{
			base:      http.DefaultTransport,
			authValue: "Basic " + encoded,
		}
	}
	// OAuth2 proxy auth would need token refresh; for now basic auth covers the primary use case

	// Build AWS config with static credentials
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(s3Config.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			s3Config.AccessKeyID,
			s3Config.SecretAccessKey,
			"", // session token not used
		)),
		awsconfig.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Build S3 client options
	s3Opts := func(o *s3.Options) {
		if s3Config.Endpoint != "" {
			o.BaseEndpoint = aws.String(s3Config.Endpoint)
		}
		o.UsePathStyle = s3Config.UsePathStyle
	}

	client := s3.NewFromConfig(cfg, s3Opts)

	return &AWSS3Uploader{
		client: client,
		bucket: s3Config.Bucket,
		logger: logger,
	}, nil
}

// UploadFile uploads a local file to S3 and returns the ETag.
func (u *AWSS3Uploader) UploadFile(ctx context.Context, localPath, s3Key string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", localPath, err)
	}
	defer func() { _ = file.Close() }()

	u.logger.Debug("Uploading to S3",
		"bucket", u.bucket,
		"key", s3Key,
		"local_path", localPath)

	result, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	if err != nil {
		return "", classifyS3Error(err)
	}

	etag := ""
	if result.ETag != nil {
		etag = *result.ETag
	}

	u.logger.Debug("S3 upload complete",
		"bucket", u.bucket,
		"key", s3Key,
		"etag", etag)

	return etag, nil
}

// GetBucket returns the configured S3 bucket name.
func (u *AWSS3Uploader) GetBucket() string {
	return u.bucket
}

// S3Error represents a classified S3 operation failure.
type S3Error struct {
	Message   string
	ErrorType models.ErrorType
}

func (e *S3Error) Error() string {
	return fmt.Sprintf("S3 error: %s", e.Message)
}

// IsRetryable returns whether this S3 error should be retried.
func (e *S3Error) IsRetryable() bool {
	return e.ErrorType == models.ErrorTypeTransient
}

// classifyS3Error wraps an S3 SDK error into a classified S3Error.
func classifyS3Error(err error) *S3Error {
	msg := err.Error()
	errType := models.ErrorTypeNonTransient

	// Transient error patterns from AWS
	transientPatterns := []string{
		"SlowDown",
		"ServiceUnavailable",
		"InternalError",
		"RequestTimeout",
		"RequestTimeTooSkewed",
		"connection reset",
		"connection refused",
		"timeout",
		"TooManyRequests",
	}

	lowerMsg := strings.ToLower(msg)
	for _, pattern := range transientPatterns {
		if strings.Contains(lowerMsg, strings.ToLower(pattern)) {
			errType = models.ErrorTypeTransient
			break
		}
	}

	return &S3Error{
		Message:   msg,
		ErrorType: errType,
	}
}

// ClassifyS3ErrorForTesting exposes classifyS3Error for unit tests.
func ClassifyS3ErrorForTesting(err error) *S3Error {
	return classifyS3Error(err)
}

// MockS3Uploader is a test double for S3Uploader.
type MockS3Uploader struct {
	Bucket       string
	UploadedKeys []string
	UploadErr    error
	ETag         string
}

// UploadFile records the upload and returns the configured response.
func (m *MockS3Uploader) UploadFile(_ context.Context, _ string, s3Key string) (string, error) {
	if m.UploadErr != nil {
		return "", m.UploadErr
	}
	m.UploadedKeys = append(m.UploadedKeys, s3Key)
	etag := m.ETag
	if etag == "" {
		etag = fmt.Sprintf("\"mock-etag-%d\"", len(m.UploadedKeys))
	}
	return etag, nil
}

// GetBucket returns the mock bucket name.
func (m *MockS3Uploader) GetBucket() string {
	return m.Bucket
}
