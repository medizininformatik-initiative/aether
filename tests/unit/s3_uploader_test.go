package unit

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

func TestS3Error_Classification_Transient(t *testing.T) {
	transientMessages := []string{
		"SlowDown: Rate exceeded",
		"ServiceUnavailable: Service is temporarily unavailable",
		"InternalError: We encountered an internal error",
		"RequestTimeout: Your socket connection was not read from",
		"connection reset by peer",
		"connection refused",
		"timeout awaiting response headers",
		"TooManyRequests: rate limit exceeded",
	}

	for _, msg := range transientMessages {
		t.Run(msg, func(t *testing.T) {
			err := services.ClassifyS3ErrorForTesting(fmt.Errorf("%s", msg))
			require.NotNil(t, err)
			assert.Equal(t, models.ErrorTypeTransient, err.ErrorType,
				"expected transient for: %s", msg)
			assert.True(t, err.IsRetryable())
		})
	}
}

func TestS3Error_Classification_NonTransient(t *testing.T) {
	nonTransientMessages := []string{
		"NoSuchBucket: The specified bucket does not exist",
		"AccessDenied: Access Denied",
		"InvalidBucketName: The specified bucket is not valid",
		"NoSuchKey: The specified key does not exist",
		"MalformedXML: The XML you provided was not well-formed",
	}

	for _, msg := range nonTransientMessages {
		t.Run(msg, func(t *testing.T) {
			err := services.ClassifyS3ErrorForTesting(fmt.Errorf("%s", msg))
			require.NotNil(t, err)
			assert.Equal(t, models.ErrorTypeNonTransient, err.ErrorType,
				"expected non-transient for: %s", msg)
			assert.False(t, err.IsRetryable())
		})
	}
}

func TestS3Error_ErrorString(t *testing.T) {
	err := &services.S3Error{
		Message:   "NoSuchBucket: bucket does not exist",
		ErrorType: models.ErrorTypeNonTransient,
	}
	assert.Equal(t, "S3 error: NoSuchBucket: bucket does not exist", err.Error())
}

func TestMockS3Uploader_Interface(t *testing.T) {
	// Verify MockS3Uploader satisfies the S3Uploader interface
	var _ services.S3Uploader = &services.MockS3Uploader{}

	mock := &services.MockS3Uploader{
		Bucket: "test-bucket",
	}

	assert.Equal(t, "test-bucket", mock.GetBucket())

	// Upload succeeds
	etag, err := mock.UploadFile(context.Background(), "/path/to/file", "job/file.ndjson")
	require.NoError(t, err)
	assert.NotEmpty(t, etag)
	assert.Equal(t, []string{"job/file.ndjson"}, mock.UploadedKeys)

	// Upload again
	etag2, err := mock.UploadFile(context.Background(), "/path/to/file2", "job/file2.ndjson")
	require.NoError(t, err)
	assert.NotEqual(t, etag, etag2, "ETags should differ")
	assert.Len(t, mock.UploadedKeys, 2)
}

func TestMockS3Uploader_Error(t *testing.T) {
	mock := &services.MockS3Uploader{
		Bucket:    "test-bucket",
		UploadErr: fmt.Errorf("simulated upload failure"),
	}

	_, err := mock.UploadFile(context.Background(), "/path/to/file", "job/file.ndjson")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated upload failure")
	assert.Empty(t, mock.UploadedKeys, "no keys should be recorded on error")
}

func TestMockS3Uploader_CustomETag(t *testing.T) {
	mock := &services.MockS3Uploader{
		Bucket: "test-bucket",
		ETag:   "\"custom-etag\"",
	}

	etag, err := mock.UploadFile(context.Background(), "/path/to/file", "job/file.ndjson")
	require.NoError(t, err)
	assert.Equal(t, "\"custom-etag\"", etag)
}
