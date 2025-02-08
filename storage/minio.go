package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/charmbracelet/log"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Storage struct {
	logger  *log.Logger
	mClient *minio.Client
}

const _BucketName = "x-likes"

func NewS3Store(ctx context.Context, logger *log.Logger) (*S3Storage, error) {
	endpoint := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("S3_SECRET_KEY")
	useSSL, err := strconv.ParseBool(os.Getenv("S3_USE_SSL"))
	if err != nil {
		logger.Info("S3_USE_SSL env not set, defaulting to 'true'")
		useSSL = true
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	exists, err := client.BucketExists(ctx, _BucketName)
	if err != nil {
		return nil, err
	}
	if !exists {
		err = client.MakeBucket(ctx, _BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, err
		}
		logger.Infof("created \"%s\" bucket", _BucketName)
	}

	return &S3Storage{
		logger:  logger,
		mClient: client,
	}, nil
}

func (s *S3Storage) StoreImage(ctx context.Context, u *url.URL, imageID string) error {
	uClone := new(url.URL)
	*uClone = *u
	q := uClone.Query()
	q.Set("name", "large")
	uClone.RawQuery = q.Encode()
	rc, err := stream(ctx, uClone)
	if err != nil {
		s.logger.Warn("failed to stream large vers, falling back", "url", uClone.String(), "id", imageID)
		rc, err = stream(ctx, u)
		if err != nil {
			return err
		}
	}
	defer rc.Close()

	format := u.Query().Get("format")
	_, err = s.mClient.PutObject(ctx, _BucketName, imageID, rc, -1, minio.PutObjectOptions{
		UserTags: map[string]string{
			"format": format,
			"name":   u.Query().Get("name"),
		},
		ContentType: contentTypeOf(format),
	})
	if err != nil {
		return err
	}

	return nil
}

func stream(ctx context.Context, url *url.URL) (io.ReadCloser, error) {
	if url == nil {
		panic("calling stream on nil URL")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode > 299 {
		return nil, fmt.Errorf("non success status: %d", res.StatusCode)
	}

	return res.Body, nil
}

func contentTypeOf(format string) string {
	switch format {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
