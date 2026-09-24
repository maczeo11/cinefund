package config

import (
	"strings"
	"testing"
)

func validConfig(env string) Config {
	return Config{
		Env: env,
		JWT: JWT{
			AccessSecret:  strings.Repeat("a", 32),
			RefreshSecret: strings.Repeat("b", 32),
		},
		Razorpay: Razorpay{
			KeyID:         "rzp_test",
			KeySecret:     "secret",
			WebhookSecret: "whsec",
		},
		Transcode: Transcode{Concurrency: 1},
	}
}

func TestValidate_DevIdentityHeaderOnlyInDevelopment(t *testing.T) {
	for _, env := range []string{"production", "staging", ""} {
		c := validConfig(env)
		c.Auth.AllowDevIdentityHeader = true
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "ALLOW_DEV_IDENTITY_HEADER") {
			t.Errorf("APP_ENV=%q: expected ALLOW_DEV_IDENTITY_HEADER to be refused, got %v", env, err)
		}
	}

	c := validConfig("development")
	c.Auth.AllowDevIdentityHeader = true
	if err := c.Validate(); err != nil {
		t.Fatalf("development should allow the dev header: %v", err)
	}
}

func TestValidate_DemoLoginAllowedInProduction(t *testing.T) {
	c := validConfig("production")
	c.Auth.DemoLogin = true
	if err := c.Validate(); err != nil {
		t.Fatalf("demo login is an explicit opt-in and valid in production: %v", err)
	}
}
