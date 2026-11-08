package main

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "defaults ok"},
		{name: "0.0.0.0 without secret fails", env: map[string]string{"DAKA_LISTEN": "0.0.0.0:8080"}, wantErr: "allow_insecure_public"},
		{
			name:    "0.0.0.0 flag without secret fails",
			env:     map[string]string{"DAKA_LISTEN": "0.0.0.0:8080", "DAKA_AUTH_ALLOW_INSECURE_PUBLIC": "true"},
			wantErr: "shared_secret",
		},
		{
			name: "0.0.0.0 flag and secret ok",
			env: map[string]string{
				"DAKA_LISTEN": "0.0.0.0:8080", "DAKA_AUTH_ALLOW_INSECURE_PUBLIC": "true", "DAKA_AUTH_SHARED_SECRET": "0123456789abcdef",
			},
		},
		{name: "old key without current fails", env: map[string]string{"DAKA_SECRET_KEY_OLD": "a-long-enough-passphrase"}, wantErr: "DAKA_SECRET_KEY_OLD"},
		{name: "malformed old key fails", env: map[string]string{"DAKA_SECRET_KEY": "a-long-enough-passphrase", "DAKA_SECRET_KEY_OLD": "tiny"}, wantErr: "invalid master key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadConfig(nil, func(k string) string { return tt.env[k] }, io.Discard)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
