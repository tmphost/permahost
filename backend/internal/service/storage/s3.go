// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

//go:generate mockgen -source=s3.go -destination=mocks/s3.go -package=mocks

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3's DeleteObjects takes at most this many keys per call.
const _s3DeleteBatch = 1000

type (
	// S3API is the part of the S3 client the S3 storage uses.
	S3API interface {
		PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
		GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
		ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
		DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
		DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	}

	// S3Config locates and authenticates against a bucket.
	S3Config struct {
		// Endpoint is set for S3-compatible services (R2, MinIO, ...) and
		// left empty for AWS.
		Endpoint string
		Region   string
		Bucket   string
		// Without an access key the AWS default credential chain is used
		// (environment, shared config, instance role).
		AccessKeyID     string
		SecretAccessKey string
		UsePathStyle    bool
	}

	s3Storage struct {
		client S3API
		bucket string
	}
)

// NewS3 returns a Service backed by an S3-compatible bucket.
func NewS3(ctx context.Context, cfg S3Config) (Service, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}
	if cfg.AccessKeyID != "" {
		awsCfg.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	return NewS3WithClient(client, cfg.Bucket), nil
}

// NewS3WithClient returns a Service using client against bucket.
func NewS3WithClient(client S3API, bucket string) Service {
	return &s3Storage{client: client, bucket: bucket}
}

func (s *s3Storage) Put(ctx context.Context, key string, data []byte) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	})
	return err
}

func (s *s3Storage) Get(ctx context.Context, key string) (io.ReadCloser, Object, error) {
	if !validKey(key) {
		return nil, Object{}, ErrInvalidKey
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	var noKey *types.NoSuchKey
	if errors.As(err, &noKey) {
		return nil, Object{}, ErrNotFound
	}
	if err != nil {
		return nil, Object{}, err
	}
	return out.Body, Object{Key: key, Size: aws.ToInt64(out.ContentLength), ModTime: aws.ToTime(out.LastModified)}, nil
}

func (s *s3Storage) List(ctx context.Context, prefix string) ([]Object, error) {
	if !validPrefix(prefix) {
		return nil, ErrInvalidKey
	}
	var out []Object
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			out = append(out, Object{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size), ModTime: aws.ToTime(o.LastModified)})
		}
	}
	return out, nil
}

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *s3Storage) DeletePrefix(ctx context.Context, prefix string) error {
	objects, err := s.List(ctx, prefix)
	if err != nil {
		return err
	}
	for start := 0; start < len(objects); start += _s3DeleteBatch {
		batch := objects[start:min(start+_s3DeleteBatch, len(objects))]
		ids := make([]types.ObjectIdentifier, len(batch))
		for i, o := range batch {
			ids[i] = types.ObjectIdentifier{Key: aws.String(o.Key)}
		}
		out, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return err
		}
		// A batch call succeeds even when single keys fail.
		if len(out.Errors) > 0 {
			return errors.New("s3: delete " + aws.ToString(out.Errors[0].Key) + ": " + aws.ToString(out.Errors[0].Message))
		}
	}
	return nil
}
