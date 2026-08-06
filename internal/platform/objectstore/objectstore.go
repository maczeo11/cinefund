// Package objectstore wraps S3-compatible storage.
//
// The API process never streams video bytes through itself: uploads go straight
// to storage with a presigned PUT, and playback segments are fetched by the
// player from presigned GET URLs. This package only ever signs URLs and moves
// small files (playlists, posters) - plus the transcoder's rendition uploads,
// which happen in a worker, not in a request.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint string
	// PublicEndpoint is what a browser can resolve. It differs from Endpoint
	// whenever the API reaches storage over an internal hostname - in docker
	// compose the API talks to "minio:9000" but the browser must be handed
	// "localhost:9000". Signing against the wrong one produces a URL that is
	// cryptographically valid and unreachable, which presents as a CORS or DNS
	// error and costs an hour to diagnose. See docs/10 §6.
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	Region         string
	UseSSL         bool
}

// Store is S3-compatible object storage.
type Store struct {
	internal *minio.Client
	public   *minio.Client
	bucket   string
}

// New builds a Store for one bucket. Two clients are constructed: one against
// the internal endpoint for real operations, one against the public endpoint
// used only to sign URLs handed to a browser.
func New(cfg Config, bucket string) (*Store, error) {
	mk := func(endpoint string) (*minio.Client, error) {
		host, secure, err := splitEndpoint(endpoint, cfg.UseSSL)
		if err != nil {
			return nil, err
		}
		return minio.New(host, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: secure,
			Region: cfg.Region,
		})
	}

	internal, err := mk(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("internal client: %w", err)
	}

	public := internal
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		if public, err = mk(cfg.PublicEndpoint); err != nil {
			return nil, fmt.Errorf("public client: %w", err)
		}
	}
	return &Store{internal: internal, public: public, bucket: bucket}, nil
}

// splitEndpoint accepts either "host:port" or a full URL.
func splitEndpoint(endpoint string, defaultSSL bool) (string, bool, error) {
	if endpoint == "" {
		return "", false, fmt.Errorf("empty endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint, defaultSSL, nil // already host:port
	}
	return u.Host, u.Scheme == "https", nil
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.internal.PutObject(ctx, s.bucket, key, r, size,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.internal.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	return obj, nil
}

// Stat verifies an object exists and returns its size. Used by POST /complete to
// confirm the client actually uploaded what it claimed.
func (s *Store) Stat(ctx context.Context, key string) (int64, string, error) {
	info, err := s.internal.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, "", fmt.Errorf("stat %s: %w", key, err)
	}
	return info.Size, info.ContentType, nil
}

// PresignedGet signs a URL against the INTERNAL endpoint. Use it for anything
// fetched server-side, including the transcoder's FFmpeg input.
func (s *Store) PresignedGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := s.internal.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("presign get %s: %w", key, err)
	}
	return u.String(), nil
}

// PresignedGetPublic signs against the browser-reachable endpoint. Use it for
// anything embedded in a playlist or returned to a client.
func (s *Store) PresignedGetPublic(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := s.public.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("presign public get %s: %w", key, err)
	}
	return u.String(), nil
}

// PresignedPut signs an upload URL for the browser.
func (s *Store) PresignedPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := s.public.PresignedPutObject(ctx, s.bucket, key, ttl)
	if err != nil {
		return "", fmt.Errorf("presign put %s: %w", key, err)
	}
	return u.String(), nil
}
