package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "daka.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		check   func(t *testing.T, c *Config)
		wantErr string
	}{
		{
			name: "defaults without file",
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "127.0.0.1:8080", c.Server.Listen)
				require.Equal(t, DriverSQLite, c.DB.Driver)
				require.Equal(t, "daka.db", c.DB.DSN.Reveal())
				require.True(t, c.Auth.SecureCookie)
				require.False(t, c.Auth.AllowInsecurePublic)
				require.Empty(t, c.Auth.Issuer)
			},
		},
		{
			name: "yaml values",
			yaml: `
server:
  listen: 0.0.0.0:9000
db:
  driver: postgres
  dsn: postgres://u@h/db
auth:
  issuer: https://idp.example.com
  allow_insecure_public: true
  shared_secret: pw
  secure_cookie: false
`,
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "0.0.0.0:9000", c.Server.Listen)
				require.Equal(t, DriverPostgres, c.DB.Driver)
				require.Equal(t, "postgres://u@h/db", c.DB.DSN.Reveal())
				require.Equal(t, "https://idp.example.com", c.Auth.Issuer)
				require.True(t, c.Auth.AllowInsecurePublic)
				require.Equal(t, "pw", c.Auth.SharedSecret.Reveal())
				require.False(t, c.Auth.SecureCookie)
			},
		},
		{
			name: "env overrides yaml",
			yaml: "server:\n  listen: 0.0.0.0:9000\nauth:\n  secure_cookie: true\n",
			env: map[string]string{
				"DAKA_LISTEN":                     "127.0.0.1:1234",
				"DAKA_DB_DRIVER":                  "postgres",
				"DAKA_DB_DSN":                     "host=x",
				"DAKA_AUTH_ISSUER":                "https://i",
				"DAKA_AUTH_ALLOW_INSECURE_PUBLIC": "true",
				"DAKA_AUTH_SHARED_SECRET":         "s3",
				"DAKA_AUTH_SECURE_COOKIE":         "false",
			},
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "127.0.0.1:1234", c.Server.Listen)
				require.Equal(t, DriverPostgres, c.DB.Driver)
				require.Equal(t, "host=x", c.DB.DSN.Reveal())
				require.Equal(t, "https://i", c.Auth.Issuer)
				require.True(t, c.Auth.AllowInsecurePublic)
				require.Equal(t, "s3", c.Auth.SharedSecret.Reveal())
				require.False(t, c.Auth.SecureCookie)
			},
		},
		{
			name: "secrets only from env",
			env:  map[string]string{"DAKA_SECRET_KEY": "k", "DAKA_TELEGRAM_BOT_TOKEN": "t"},
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "k", c.Secrets.SecretKey.Reveal())
				require.Equal(t, "t", c.Secrets.TelegramBotToken.Reveal())
			},
		},
		{name: "secret key in yaml rejected", yaml: "secret_key: oops\n", wantErr: "secret_key"},
		{name: "telegram token in yaml rejected", yaml: "telegram_bot_token: x\n", wantErr: "telegram_bot_token"},
		{name: "unknown yaml key rejected", yaml: "bogus: 1\n", wantErr: "bogus"},
		{name: "bad driver", yaml: "db:\n  driver: mysql\n", wantErr: "db.driver"},
		{name: "bad bool env", env: map[string]string{"DAKA_AUTH_SECURE_COOKIE": "maybe"}, wantErr: "DAKA_AUTH_SECURE_COOKIE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := ""
			if tt.yaml != "" {
				path = writeYAML(t, tt.yaml)
			}
			c, err := Load(path, envFrom(tt.env))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, c)
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), envFrom(nil))
	require.Error(t, err)
}

func TestSecretsNeverRendered(t *testing.T) {
	c, err := Load("", envFrom(map[string]string{
		"DAKA_SECRET_KEY":         "KEY-VALUE",
		"DAKA_TELEGRAM_BOT_TOKEN": "TG-VALUE",
		"DAKA_AUTH_SHARED_SECRET": "PW-VALUE",
		"DAKA_DB_DSN":             "postgres://user:DSNPASS@h/db",
	}))
	require.NoError(t, err)
	js, err := json.Marshal(c)
	require.NoError(t, err)
	outs := []string{
		c.String(), fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c),
		fmt.Sprintf("%v", *c), fmt.Sprintf("%+v", c.Secrets), string(js),
		fmt.Sprint(c.Secrets.SecretKey), fmt.Sprint(c.Auth.SharedSecret),
	}
	for _, o := range outs {
		for _, s := range []string{"KEY-VALUE", "TG-VALUE", "PW-VALUE", "DSNPASS"} {
			require.False(t, strings.Contains(o, s), "leaked %s in %q", s, o)
		}
	}
}
