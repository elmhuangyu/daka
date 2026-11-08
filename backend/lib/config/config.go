package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Supported database drivers.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

// DefaultListen is the default listen address (loopback only, see DESIGN 8).
const DefaultListen = "127.0.0.1:8080"

const redacted = "[REDACTED]"

// Secret is a string that never renders its value through fmt, JSON or text
// marshalling. Use Reveal to obtain the raw value deliberately.
type Secret string

// Reveal returns the raw secret value.
func (s Secret) Reveal() string { return string(s) }

// String implements fmt.Stringer.
func (s Secret) String() string { return s.mask() }

// GoString implements fmt.GoStringer.
func (s Secret) GoString() string { return s.mask() }

// MarshalJSON implements json.Marshaler.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(s.mask())), nil }

// MarshalText implements encoding.TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.mask()), nil }

// MarshalYAML implements yaml.InterfaceMarshaler.
func (s Secret) MarshalYAML() (any, error) { return s.mask(), nil }

func (s Secret) mask() string {
	if s == "" {
		return ""
	}
	return redacted
}

// Config is the complete application configuration.
type Config struct {
	// Timezone is the IANA timezone of the bootstrapped single-user account
	// (only used when that account is first created).
	Timezone string        `yaml:"timezone" json:"timezone"`
	Server   ServerConfig  `yaml:"server" json:"server"`
	DB       DBConfig      `yaml:"db" json:"db"`
	Auth     AuthConfig    `yaml:"auth" json:"auth"`
	Secrets  SecretsConfig `yaml:"-" json:"secrets"`
}

// ServerConfig configures the HTTP listener.
type ServerConfig struct {
	Listen string `yaml:"listen" json:"listen"`
	// StaticDir, when set, is a directory with the built frontend served at
	// "/" (SPA fallback to index.html).
	StaticDir string `yaml:"static_dir" json:"static_dir"`
	// TrustedProxies lists IPs / CIDRs of reverse proxies whose X-Forwarded-For
	// header is honored. Empty (default): only the TCP peer address is used.
	TrustedProxies []string `yaml:"trusted_proxies" json:"trusted_proxies"`
	// RateLimit configures the global per-client token bucket.
	RateLimit RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
}

// RateLimitConfig is the global request rate limit (per client IP).
type RateLimitConfig struct {
	// Disabled turns the global limiter off.
	Disabled bool `yaml:"disabled" json:"disabled"`
	// RPS is the sustained requests per second per client.
	RPS float64 `yaml:"rps" json:"rps"`
	// Burst is the bucket size (maximum burst).
	Burst int `yaml:"burst" json:"burst"`
}

// Default global rate limit.
const (
	DefaultRateLimitRPS   = 20.0
	DefaultRateLimitBurst = 60
)

// DefaultTimezone is the timezone given to the bootstrapped default user.
const DefaultTimezone = "UTC"

// DBConfig selects and locates the database. DSN is a file path for sqlite
// and a connection string for postgres; it may embed credentials and is
// therefore a Secret.
type DBConfig struct {
	Driver string `yaml:"driver" json:"driver"`
	DSN    Secret `yaml:"dsn" json:"dsn"`
}

// AuthConfig holds the auth-related settings (rules are enforced elsewhere).
type AuthConfig struct {
	Issuer              string `yaml:"issuer" json:"issuer"`
	AllowInsecurePublic bool   `yaml:"allow_insecure_public" json:"allow_insecure_public"`
	SharedSecret        Secret `yaml:"shared_secret" json:"shared_secret"`
	// SecureCookie sets the Secure flag on session cookies. Disable only for
	// plain-HTTP local / LAN deployments.
	SecureCookie bool `yaml:"secure_cookie" json:"secure_cookie"`
}

// SecretsConfig holds values that come ONLY from environment variables.
// It is never read from YAML.
type SecretsConfig struct {
	SecretKey Secret `json:"secret_key"`
	// SecretKeyOld holds comma-separated retired master keys (DAKA_SECRET_KEY_OLD)
	// that can still decrypt during key rotation.
	SecretKeyOld     Secret `json:"secret_key_old"`
	TelegramBotToken Secret `json:"telegram_bot_token"`
}

// Default returns the configuration used when nothing is specified.
func Default() Config {
	return Config{
		Timezone: DefaultTimezone,
		Server: ServerConfig{
			Listen:    DefaultListen,
			RateLimit: RateLimitConfig{RPS: DefaultRateLimitRPS, Burst: DefaultRateLimitBurst},
		},
		DB:   DBConfig{Driver: DriverSQLite, DSN: "daka.db"},
		Auth: AuthConfig{SecureCookie: true},
	}
}

// String renders the config for logging; secrets are redacted.
func (c Config) String() string {
	return fmt.Sprintf(
		"Config{server.listen=%q db.driver=%q db.dsn=%s auth.issuer=%q auth.allow_insecure_public=%t auth.shared_secret=%s auth.secure_cookie=%t secret_key=%s secret_key_old=%s telegram_bot_token=%s}",
		c.Server.Listen, c.DB.Driver, c.DB.DSN, c.Auth.Issuer, c.Auth.AllowInsecurePublic,
		c.Auth.SharedSecret, c.Auth.SecureCookie, c.Secrets.SecretKey, c.Secrets.SecretKeyOld, c.Secrets.TelegramBotToken,
	)
}

// GoString implements fmt.GoStringer so %#v also redacts.
func (c Config) GoString() string { return c.String() }

// Load builds the configuration: defaults, then the YAML file at path (skipped
// when empty), then environment overrides read through getenv (os.Getenv when
// nil). Unknown YAML keys are rejected, so secrets cannot be smuggled into YAML.
// Only syntax and enum values are checked here; callers must run Validate for
// the listen/auth policy and DSN rules.
func Load(path string, getenv func(string) string) (*Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	c := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		def := c
		dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
		if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
		if reflect.DeepEqual(c, Config{}) { // empty / comment-only document decodes to the zero value
			c = def
		}
	}
	if err := applyEnv(&c, getenv); err != nil {
		return nil, err
	}
	switch c.DB.Driver {
	case DriverSQLite, DriverPostgres:
	default:
		return nil, fmt.Errorf("invalid db.driver %q (want %q or %q)", c.DB.Driver, DriverSQLite, DriverPostgres)
	}
	return &c, nil
}

func applyEnv(c *Config, getenv func(string) string) error {
	str := func(key string, dst *string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	secret := func(key string, dst *Secret) {
		if v := getenv(key); v != "" {
			*dst = Secret(v)
		}
	}
	boolean := func(key string, dst *bool) error {
		v := getenv(key)
		if v == "" {
			return nil
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid %s: %q is not a boolean", key, v)
		}
		*dst = b
		return nil
	}

	str("DAKA_LISTEN", &c.Server.Listen)
	str("DAKA_STATIC_DIR", &c.Server.StaticDir)
	str("DAKA_TIMEZONE", &c.Timezone)
	str("DAKA_DB_DRIVER", &c.DB.Driver)
	secret("DAKA_DB_DSN", &c.DB.DSN)
	str("DAKA_AUTH_ISSUER", &c.Auth.Issuer)
	secret("DAKA_AUTH_SHARED_SECRET", &c.Auth.SharedSecret)
	secret("DAKA_SECRET_KEY", &c.Secrets.SecretKey)
	secret("DAKA_SECRET_KEY_OLD", &c.Secrets.SecretKeyOld)
	secret("DAKA_TELEGRAM_BOT_TOKEN", &c.Secrets.TelegramBotToken)
	if v := getenv("DAKA_TRUSTED_PROXIES"); v != "" {
		c.Server.TrustedProxies = nil
		for _, e := range strings.Split(v, ",") {
			if e = strings.TrimSpace(e); e != "" {
				c.Server.TrustedProxies = append(c.Server.TrustedProxies, e)
			}
		}
	}
	if v := getenv("DAKA_RATE_LIMIT_RPS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("invalid DAKA_RATE_LIMIT_RPS: %q is not a number", v)
		}
		c.Server.RateLimit.RPS = f
	}
	if v := getenv("DAKA_RATE_LIMIT_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid DAKA_RATE_LIMIT_BURST: %q is not an integer", v)
		}
		c.Server.RateLimit.Burst = n
	}
	if err := boolean("DAKA_RATE_LIMIT_DISABLED", &c.Server.RateLimit.Disabled); err != nil {
		return err
	}
	if err := boolean("DAKA_AUTH_ALLOW_INSECURE_PUBLIC", &c.Auth.AllowInsecurePublic); err != nil {
		return err
	}
	return boolean("DAKA_AUTH_SECURE_COOKIE", &c.Auth.SecureCookie)
}
