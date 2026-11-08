package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		listen  string
		allow   bool
		secret  string
		issuer  string
		wantErr string
	}{
		{name: "ipv4 loopback", listen: "127.0.0.1:8080"},
		{name: "localhost", listen: "localhost:8080"},
		{name: "ipv6 loopback", listen: "[::1]:8080"},
		{name: "0.0.0.0 no flag", listen: "0.0.0.0:8080", wantErr: "allow_insecure_public"},
		{name: "0.0.0.0 flag no secret", listen: "0.0.0.0:8080", allow: true, wantErr: "shared_secret"},
		{name: "0.0.0.0 secret no flag", listen: "0.0.0.0:8080", secret: "0123456789abcdef", wantErr: "allow_insecure_public"},
		{name: "0.0.0.0 flag and secret", listen: "0.0.0.0:8080", allow: true, secret: "0123456789abcdef"},
		{name: "[::] no flag", listen: "[::]:8080", wantErr: "allow_insecure_public"},
		{name: "[::] flag no secret", listen: "[::]:8080", allow: true, wantErr: "shared_secret"},
		{name: "empty host", listen: ":8080", wantErr: "allow_insecure_public"},
		{name: "empty host flag no secret", listen: ":8080", allow: true, wantErr: "shared_secret"},
		{name: "empty host flag and secret", listen: ":8080", allow: true, secret: "0123456789abcdef"},
		{name: "public ip", listen: "203.0.113.5:80", wantErr: "allow_insecure_public"},
		{name: "hostname flag no secret", listen: "daka.example.com:80", allow: true, wantErr: "shared_secret"},
		{name: "hostname ok", listen: "daka.example.com:80", allow: true, secret: "0123456789abcdef"},
		{name: "missing port", listen: "127.0.0.1", wantErr: "server.listen"},
		{name: "short secret", listen: "127.0.0.1:8080", secret: "short", wantErr: "at least 16"},
		{name: "oidc mode public bind", listen: "0.0.0.0:8080", issuer: "https://idp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.Server.Listen = tt.listen
			c.Auth.AllowInsecurePublic = tt.allow
			c.Auth.SharedSecret = Secret(tt.secret)
			c.Auth.Issuer = tt.issuer
			if tt.issuer != "" {
				c.Auth.ClientID = "daka"
				c.Auth.RedirectURL = "https://daka.example.com/api/auth/oidc/callback"
			}
			err := c.Validate()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateSQLiteDSN(t *testing.T) {
	tests := []struct {
		name, driver, dsn, wantErr string
	}{
		{"plain path", DriverSQLite, "data/daka.db", ""},
		{"empty", DriverSQLite, "", "db.dsn"},
		{"file prefix", DriverSQLite, "file:daka.db", "plain file path"},
		{"query", DriverSQLite, "daka.db?_pragma=x", "plain file path"},
		{"postgres query ok", DriverPostgres, "postgres://u@h/db?sslmode=disable", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.DB.Driver = tt.driver
			c.DB.DSN = Secret(tt.dsn)
			err := c.Validate()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLoadEmptyFile(t *testing.T) {
	c, err := Load(writeYAML(t, ""), envFrom(nil))
	require.NoError(t, err)
	require.Equal(t, DefaultListen, c.Server.Listen)
}

func TestValidateOIDC(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*Config)
		wantErr string
	}{
		{"ok", func(*Config) {}, ""},
		{"ok without client secret (public client)", func(c *Config) { c.Auth.ClientSecret = "" }, ""},
		{"missing client id", func(c *Config) { c.Auth.ClientID = "" }, "auth.client_id"},
		{"missing redirect", func(c *Config) { c.Auth.RedirectURL = "" }, "auth.redirect_url"},
		{"relative redirect", func(c *Config) { c.Auth.RedirectURL = "/cb" }, "auth.redirect_url"},
		{"bad issuer", func(c *Config) { c.Auth.Issuer = "idp.example.com" }, "auth.issuer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.Auth.Issuer = "https://idp.example.com"
			c.Auth.ClientID = "daka"
			c.Auth.ClientSecret = "x"
			c.Auth.RedirectURL = "https://daka.example.com/api/auth/oidc/callback"
			tt.mut(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
