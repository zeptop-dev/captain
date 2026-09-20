// Package config loads Captain's YAML configuration.
package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration.
type Config struct {
	// Path is where this config was read from; backups put a copy of it
	// in the encrypted off-site archive (it holds base_url and the
	// payment keys, which the database does not).
	Path     string `yaml:"-"`
	Listen   string `yaml:"listen"`   // listen address; default 127.0.0.1:8080, or :443 when tls is on
	BaseURL  string `yaml:"base_url"` // public URL, used in subscription links and payment callbacks
	DataDir  string `yaml:"data_dir"`
	LogLevel string `yaml:"log_level"`
	// TrustedProxies are the reverse proxies (IPs or CIDRs) whose
	// X-Forwarded-For / X-Real-IP is believed for rate limits, the admin
	// allow-list and order IPs. Empty = any loopback or private peer.
	TrustedProxies []string `yaml:"trusted_proxies"`

	// TLS lets Captain terminate HTTPS itself. With Auto it obtains and renews
	// a Let's Encrypt certificate for the base_url host (ports 80 and 443
	// must be reachable from the internet); with Cert/Key it serves your
	// own files. Off by default: put a reverse proxy in front instead.
	TLS struct {
		Auto       bool   `yaml:"auto"`
		Email      string `yaml:"email"`       // ACME account contact (recommended)
		Domain     string `yaml:"domain"`      // default: host of base_url
		HTTPListen string `yaml:"http_listen"` // ACME HTTP-01 + redirect to https; default :80
		// CloudflareToken switches to DNS-01 and obtains a wildcard for
		// tls.domain as well, so port 80 is no longer needed.
		CloudflareToken string `yaml:"cloudflare_token"`
		Staging         bool   `yaml:"staging"` // Let's Encrypt staging CA (test certificates)
		Cert            string `yaml:"cert"`
		Key             string `yaml:"key"`
	} `yaml:"tls"`

	Database struct {
		// Driver is "sqlite"; nothing else is implemented (see
		// docs/COMPATIBILITY.md). The key exists so an installation can
		// be explicit, not to offer a choice.
		Driver string `yaml:"driver"`
		DSN    string `yaml:"dsn"` // sqlite: the database file path
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
		Alipay *struct { // 支付宝当面付 (scan-to-pay QR)
			AppID      string `yaml:"app_id"`
			PrivateKey string `yaml:"private_key"` // 应用私钥, PEM or bare base64
			PublicKey  string `yaml:"public_key"`  // 支付宝公钥, PEM or bare base64
			Subject    string `yaml:"subject"`     // bill line; default: plan name
		} `yaml:"alipay"`
		Coinbase *struct { // Coinbase Commerce
			APIKey        string `yaml:"api_key"`
			WebhookSecret string `yaml:"webhook_secret"`
			Currency      string `yaml:"currency"` // local price currency; default CNY
		} `yaml:"coinbase"`
		CoinPayments *struct {
			MerchantID string `yaml:"merchant_id"`
			PublicKey  string `yaml:"public_key"`
			PrivateKey string `yaml:"private_key"`
			IPNSecret  string `yaml:"ipn_secret"`
			Currency   string `yaml:"currency"` // price currency; default USD
		} `yaml:"coinpayments"`
		BTCPay *struct { // BTCPay Server (Greenfield API)
			URL           string `yaml:"url"`
			StoreID       string `yaml:"store_id"`
			APIKey        string `yaml:"api_key"`
			WebhookSecret string `yaml:"webhook_secret"`
			Currency      string `yaml:"currency"` // invoice currency; default CNY
		} `yaml:"btcpay"`
		MGate *struct {
			URL            string `yaml:"url"`
			AppID          string `yaml:"app_id"`
			AppSecret      string `yaml:"app_secret"`
			SourceCurrency string `yaml:"source_currency"` // e.g. CNY
		} `yaml:"mgate"`
	} `yaml:"payments"`

	Portal struct {
		Registration bool `yaml:"registration"` // allow self sign-up
	} `yaml:"portal"`

	Limits struct {
		// EnforceDevices drops a user from nodes while more distinct IPs than
		// the plan allows were seen in the last few minutes. Default true.
		EnforceDevices *bool `yaml:"enforce_devices"`
	} `yaml:"limits"`

	SiteName string `yaml:"site_name"`
	// MinVersion refuses a self-update or rollback below this release, so
	// a rollback cannot walk back past a migration or a security fix.
	MinVersion string `yaml:"min_version"`
	Version    string `yaml:"-"` // set by main
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
	if abs, err := filepath.Abs(path); err == nil {
		c.Path = abs
	} else {
		c.Path = path
	}
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

// TLSEnabled reports whether Captain serves HTTPS itself.
func (c *Config) TLSEnabled() bool { return c.TLS.Auto || (c.TLS.Cert != "" && c.TLS.Key != "") }

// TLSDomain is the certificate host: tls.domain or the base_url host.
func (c *Config) TLSDomain() string {
	if c.TLS.Domain != "" {
		return c.TLS.Domain
	}
	if u, err := url.Parse(c.BaseURL); err == nil {
		return u.Hostname()
	}
	return ""
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8080"
		if c.TLSEnabled() {
			c.Listen = ":443"
		}
	}
	if c.TLS.HTTPListen == "" {
		c.TLS.HTTPListen = ":80"
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

// EnforceDevices reports whether device limits are enforced.
func (c *Config) EnforceDevices() bool {
	return c.Limits.EnforceDevices == nil || *c.Limits.EnforceDevices
}
