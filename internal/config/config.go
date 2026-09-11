// Package config loads Captain's YAML configuration.
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration.
type Config struct {
	Listen   string `yaml:"listen"`   // HTTP listen address, default 127.0.0.1:8080
	BaseURL  string `yaml:"base_url"` // public URL, used in subscription links and payment callbacks
	DataDir  string `yaml:"data_dir"`
	LogLevel string `yaml:"log_level"`

	Database struct {
		Driver string `yaml:"driver"` // "sqlite" (default) or "postgres"
		DSN    string `yaml:"dsn"`    // sqlite: file path; postgres: connection string
	} `yaml:"database"`

	Agent struct {
		PullSeconds int `yaml:"pull_seconds"` // how often bosun polls when long-poll is unavailable
		PushSeconds int `yaml:"push_seconds"`
	} `yaml:"agent"`

	Payments struct {
		EPay *struct {
			Version            string `yaml:"version"` // v1 (MD5, default) | v2 (RSA)
			URL                string `yaml:"url"`     // gateway base URL, e.g. https://zpayz.cn or https://www.ezfp.cn
			PID                string `yaml:"pid"`
			Key                string `yaml:"key"`                  // v1
			Type               string `yaml:"type"`                 // alipay | wxpay | "" (cashier chooses)
			MerchantPrivateKey string `yaml:"merchant_private_key"` // v2, PEM or bare base64
			PlatformPublicKey  string `yaml:"platform_public_key"`  // v2, PEM or bare base64
		} `yaml:"epay"`
		Stripe *struct {
			SecretKey     string `yaml:"secret_key"`
			WebhookSecret string `yaml:"webhook_secret"`
			Currency      string `yaml:"currency"`
		} `yaml:"stripe"`
	} `yaml:"payments"`

	Portal struct {
		Registration bool `yaml:"registration"` // allow self sign-up
	} `yaml:"portal"`

	SiteName string `yaml:"site_name"`
	Version  string `yaml:"-"` // set by main
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	c.applyDefaults()
	if c.BaseURL == "" {
		return nil, fmt.Errorf("config: base_url is required (public URL of this panel)")
	}
	return &c, nil
}

// Default returns a config with defaults, for commands that run without a file.
func Default() *Config {
	c := &Config{}
	c.applyDefaults()
	return c
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8080"
	}
	if c.DataDir == "" {
		c.DataDir = "/var/lib/captain"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.DSN == "" && c.Database.Driver == "sqlite" {
		c.Database.DSN = c.DataDir + "/captain.db"
	}
	if c.Agent.PullSeconds == 0 {
		c.Agent.PullSeconds = 60
	}
	if c.Agent.PushSeconds == 0 {
		c.Agent.PushSeconds = 60
	}
	if c.SiteName == "" {
		c.SiteName = "Captain"
	}
}
