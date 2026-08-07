// Package config parses environment into a typed struct at boot.
package config

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	Env  string `env:"APP_ENV" envDefault:"development"`
	Port int    `env:"PORT"    envDefault:"8080"`

	Postgres  Postgres
	Redis     Redis
	Kafka     Kafka
	S3        S3
	JWT       JWT
	Razorpay  Razorpay
	Transcode Transcode
}

type Postgres struct {
	DSN      string `env:"POSTGRES_DSN,required"`
	MaxConns int32  `env:"POSTGRES_MAX_CONNS" envDefault:"10"`
}

type Redis struct {
	Addr string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	DB   int    `env:"REDIS_DB"   envDefault:"0"`
}

type Kafka struct {
	Brokers []string `env:"KAFKA_BROKERS" envSeparator:"," envDefault:"localhost:9092"`
}

type S3 struct {
	Endpoint string `env:"S3_ENDPOINT" envDefault:"http://localhost:9000"`
	PublicEndpoint  string `env:"S3_PUBLIC_ENDPOINT" envDefault:"http://localhost:9000"`
	AccessKey       string `env:"S3_ACCESS_KEY,required"`
	SecretKey       string `env:"S3_SECRET_KEY,required"`
	Region          string `env:"S3_REGION" envDefault:"us-east-1"`
	BucketOriginals string `env:"S3_BUCKET_ORIGINALS" envDefault:"cinefund-originals"`
	BucketMedia     string `env:"S3_BUCKET_MEDIA"     envDefault:"cinefund-media"`
}

type JWT struct {
	AccessSecret  string        `env:"JWT_ACCESS_SECRET,required"`
	RefreshSecret string        `env:"JWT_REFRESH_SECRET,required"`
	AccessTTL     time.Duration `env:"JWT_ACCESS_TTL"  envDefault:"15m"`
	RefreshTTL    time.Duration `env:"JWT_REFRESH_TTL" envDefault:"720h"`
}

type Razorpay struct {
	KeyID         string `env:"RAZORPAY_KEY_ID"`
	KeySecret     string `env:"RAZORPAY_KEY_SECRET"`
	WebhookSecret string `env:"RAZORPAY_WEBHOOK_SECRET"`
}

type Transcode struct {
	Concurrency int    `env:"TRANSCODE_CONCURRENCY" envDefault:"2"`
	TmpDir      string `env:"TRANSCODE_TMP_DIR"     envDefault:"./tmp/transcode"`
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// UseFakeGateway returns true when no Razorpay creds are set (dev mode).
func (c Config) UseFakeGateway() bool {
	return !c.IsProduction() && c.Razorpay.KeyID == ""
}

// Validate checks that required config values make sense.
func (c Config) Validate() error {
	var errs []error

	if len(c.JWT.AccessSecret) < 32 {
		errs = append(errs, errors.New("JWT_ACCESS_SECRET must be at least 32 bytes"))
	}
	if len(c.JWT.RefreshSecret) < 32 {
		errs = append(errs, errors.New("JWT_REFRESH_SECRET must be at least 32 bytes"))
	}
	// if they match, an access token works as a refresh token
	if c.JWT.AccessSecret == c.JWT.RefreshSecret {
		errs = append(errs, errors.New("JWT access and refresh secrets must differ"))
	}
	if c.Transcode.Concurrency < 1 {
		errs = append(errs, errors.New("TRANSCODE_CONCURRENCY must be >= 1"))
	}
	if c.IsProduction() {
		if c.Razorpay.WebhookSecret == "" {
			errs = append(errs, errors.New("RAZORPAY_WEBHOOK_SECRET is required in production"))
		}
		if c.Razorpay.KeyID == "" || c.Razorpay.KeySecret == "" {
			errs = append(errs, errors.New("Razorpay credentials are required in production"))
		}
	}
	return errors.Join(errs...)
}

// MustLoad parses env and exits if invalid.
func MustLoad() Config {
	// .env is optional — production sets env vars directly
	_ = godotenv.Load()

	var c Config
	if err := env.Parse(&c); err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := c.Validate(); err != nil {
		log.Fatalf("config invalid:\n%s", fmt.Sprintf("%v", err))
	}
	return c
}
