package config

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSecurityDefaultsAndYAML(t *testing.T) {
	c, err := Load("", envFrom(nil))
	require.NoError(t, err)
	require.Empty(t, c.Server.TrustedProxies)
	require.Equal(t, DefaultRateLimitRPS, c.Server.RateLimit.RPS)
	require.Equal(t, DefaultRateLimitBurst, c.Server.RateLimit.Burst)

	p := writeYAML(t, "server:\n  trusted_proxies: [\"10.0.0.0/8\", \"192.168.1.5\"]\n  rate_limit:\n    burst: 5\n")
	c, err = Load(p, envFrom(map[string]string{"DAKA_SECRET_KEY_OLD": "oldkey-oldkey-oldkey"}))
	require.NoError(t, err)
	require.NoError(t, c.Validate())
	require.Equal(t, []string{"10.0.0.0/8", "192.168.1.5"}, c.Server.TrustedProxies)
	require.Equal(t, 5, c.Server.RateLimit.Burst)
	require.Equal(t, DefaultRateLimitRPS, c.Server.RateLimit.RPS) // kept default
	require.Equal(t, "oldkey-oldkey-oldkey", c.Secrets.SecretKeyOld.Reveal())
	require.NotContains(t, c.String(), "oldkey")
}

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies([]string{"10.1.2.3/8", "192.168.1.5", " ::1 ", "::ffff:10.0.0.1"})
	require.NoError(t, err)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.5/32"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("10.0.0.1/32"),
	}, got)

	for _, bad := range []string{"", "nope", "10.0.0.0/33", "example.com"} {
		_, err := ParseTrustedProxies([]string{bad})
		require.ErrorContains(t, err, "trusted_proxies", bad)
	}
}

func TestValidateSecurity(t *testing.T) {
	c := Default()
	c.Server.TrustedProxies = []string{"bogus"}
	require.ErrorContains(t, c.Validate(), "trusted_proxies")

	c = Default()
	c.Server.RateLimit.RPS = 0
	require.ErrorContains(t, c.Validate(), "rate_limit")
	c.Server.RateLimit.Disabled = true
	require.NoError(t, c.Validate())

	c = Default()
	c.Server.RateLimit.Burst = 0
	require.ErrorContains(t, c.Validate(), "rate_limit")
}

func TestSecurityEnvOverrides(t *testing.T) {
	c, err := Load("", envFrom(map[string]string{
		"DAKA_TRUSTED_PROXIES":     "10.0.0.0/8, 192.168.1.5,",
		"DAKA_RATE_LIMIT_RPS":      "2.5",
		"DAKA_RATE_LIMIT_BURST":    "7",
		"DAKA_RATE_LIMIT_DISABLED": "true",
	}))
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/8", "192.168.1.5"}, c.Server.TrustedProxies)
	require.Equal(t, RateLimitConfig{Disabled: true, RPS: 2.5, Burst: 7}, c.Server.RateLimit)

	for _, env := range []map[string]string{
		{"DAKA_RATE_LIMIT_RPS": "fast"},
		{"DAKA_RATE_LIMIT_BURST": "1.5"},
		{"DAKA_RATE_LIMIT_DISABLED": "maybe"},
	} {
		_, err := Load("", envFrom(env))
		require.Error(t, err, env)
	}
	c, err = Load("", envFrom(map[string]string{"DAKA_TRUSTED_PROXIES": "bogus"}))
	require.NoError(t, err)
	require.ErrorContains(t, c.Validate(), "trusted_proxies")
}
