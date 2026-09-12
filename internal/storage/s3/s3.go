// Package s3 is an S3-API-compatible ObjectStore implementation — works
// against AWS S3, MinIO, and Cloudflare R2 without code changes, only
// config (docs/architecture-cookbook.md §8.1).
//
// EXPERIMENTAL, and not reachable from the running server.
// adapters/cli/serve.go only ever constructs internal/storage/filesystem;
// there is no flag, environment variable, or config path that selects this
// backend, and its tests exercise it against a fake rather than a real
// S3/MinIO endpoint. Treat it as unfinished work rather than a deployment
// option: choosing it today is not possible, and the first thing anyone
// wiring it up must add is an integration test against a real endpoint.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/koduj-dev/context-commander/internal/storage"
)

type Config struct {
	Bucket string
	Region string

	// Endpoint and UsePathStyle are set for S3-compatible backends like
	// MinIO/R2. Leave both zero-valued for real AWS S3.
	Endpoint     string
	UsePathStyle bool

	AccessKeyID     string
	SecretAccessKey string
}

type Store struct {
	client *s3.Client
	bucket string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1" // MinIO/R2 ignore this; required by the SDK regardless.
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("storage/s3: load config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})

	return &Store{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the configured bucket if it doesn't already exist —
// idempotent, safe to call on every startup for local/dev use.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}

	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	var alreadyOwned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &alreadyOwned) {
		return fmt.Errorf("storage/s3: create bucket %q: %w", s.bucket, err)
	}
	return nil
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64) (string, error) {
	if key == "" {
		return "", storage.ErrInvalidKey
	}

	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return "", fmt.Errorf("storage/s3: put %q: %w", key, err)
	}

	etag := ""
	if out.ETag != nil {
		etag = strings.Trim(*out.ETag, `"`)
	}
	return etag, nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, storage.ErrInvalidKey
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("storage/s3: get %q: %w", key, err)
	}
	return out.Body, nil
}

// Delete removes key. Like the filesystem backend, deleting an
// already-absent key is not an error — S3's DeleteObject itself already
// behaves this way (docs/adr/0013's tombstone-then-cleanup lifecycle calls
// Delete only after the owning DB row is already gone).
func (s *Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return storage.ErrInvalidKey
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage/s3: delete %q: %w", key, err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	var out []storage.ObjectInfo
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("storage/s3: list %q: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			info := storage.ObjectInfo{Key: aws.ToString(obj.Key)}
			if obj.LastModified != nil {
				info.LastModified = *obj.LastModified
			}
			out = append(out, info)
		}
	}
	return out, nil
}

var _ storage.ObjectStore = (*Store)(nil)
