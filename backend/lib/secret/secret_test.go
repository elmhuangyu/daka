package secret

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	keyA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // hex
	keyB = "correct horse battery staple"                                     // passphrase
	ctx  = "user:1:external_calendar"
)

func ring(t *testing.T, cur string, old ...string) *Keyring {
	t.Helper()
	k, err := NewKeyring(cur, old...)
	require.NoError(t, err)
	return k
}

func TestRoundTrip(t *testing.T) {
	k := ring(t, keyA)
	env, err := k.Encrypt([]byte("https://example.com/cal.ics?token=s3cret"), ctx)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(env, "v1:"+k.CurrentKeyID()+":"))
	require.NotContains(t, env, "example.com")
	pt, err := k.Decrypt(env, ctx)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/cal.ics?token=s3cret", string(pt))
}

func TestEmptyPlaintext(t *testing.T) {
	k := ring(t, keyA)
	env, err := k.Encrypt(nil, ctx)
	require.NoError(t, err)
	pt, err := k.Decrypt(env, ctx)
	require.NoError(t, err)
	require.Empty(t, pt)
}

func TestKeyFormats(t *testing.T) {
	raw, _ := hex.DecodeString(keyA)
	forms := []string{
		keyA,
		base64.StdEncoding.EncodeToString(raw),
		base64.RawURLEncoding.EncodeToString(raw),
	}
	id := ring(t, forms[0]).CurrentKeyID()
	for _, f := range forms {
		require.Equal(t, id, ring(t, f).CurrentKeyID(), f)
	}
	_ = ring(t, keyB)
	_, err := NewKeyring("short")
	require.ErrorIs(t, err, ErrInvalidKey)
}

func TestTamperDetection(t *testing.T) {
	k := ring(t, keyA)
	env, _ := k.Encrypt([]byte("hello"), ctx)
	parts := strings.Split(env, ":")
	blob, _ := base64.StdEncoding.DecodeString(parts[2])
	enc := base64.StdEncoding.EncodeToString

	flipped := append([]byte(nil), blob...)
	flipped[len(flipped)-1] ^= 0x01
	_, err := k.Decrypt("v1:"+parts[1]+":"+enc(flipped), ctx)
	require.ErrorIs(t, err, ErrDecrypt)

	flippedNonce := append([]byte(nil), blob...)
	flippedNonce[0] ^= 0x80
	_, err = k.Decrypt("v1:"+parts[1]+":"+enc(flippedNonce), ctx)
	require.ErrorIs(t, err, ErrDecrypt)

	_, err = k.Decrypt("v1:"+parts[1]+":"+enc(blob[:len(blob)-3]), ctx)
	require.ErrorIs(t, err, ErrDecrypt)
	_, err = k.Decrypt("v1:"+parts[1]+":"+enc(blob[:5]), ctx)
	require.ErrorIs(t, err, ErrInvalidEnvelope)

	_, err = k.Decrypt("v1:deadbeef:"+parts[2], ctx)
	require.ErrorIs(t, err, ErrUnknownKey)

	for _, bad := range []string{"", "garbage", "v2:" + parts[1] + ":" + parts[2], "v1:" + parts[1], "v1:" + parts[1] + ":!!!"} {
		_, err = k.Decrypt(bad, ctx)
		require.ErrorIs(t, err, ErrInvalidEnvelope, bad)
	}
}

func TestWrongKey(t *testing.T) {
	a, b := ring(t, keyA), ring(t, keyB)
	env, _ := a.Encrypt([]byte("x"), ctx)
	_, err := b.Decrypt(env, ctx)
	require.ErrorIs(t, err, ErrUnknownKey)
	// forge the id so the wrong key is actually used
	forged := strings.Replace(env, a.CurrentKeyID(), b.CurrentKeyID(), 1)
	_, err = b.Decrypt(forged, ctx)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestAADBinding(t *testing.T) {
	k := ring(t, keyA)
	env, _ := k.Encrypt([]byte("secret url"), "user:1:external_calendar")
	_, err := k.Decrypt(env, "user:2:external_calendar")
	require.ErrorIs(t, err, ErrDecrypt)
	_, err = k.Decrypt(env, "")
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestRotation(t *testing.T) {
	oldK := ring(t, keyB)
	env, err := oldK.Encrypt([]byte("payload"), ctx)
	require.NoError(t, err)
	oldID := oldK.CurrentKeyID()

	k, err := ParseKeyring(keyA, " "+keyB+" , ")
	require.NoError(t, err)
	require.NotEqual(t, oldID, k.CurrentKeyID())

	pt, err := k.Decrypt(env, ctx)
	require.NoError(t, err)
	require.Equal(t, "payload", string(pt))

	re, changed, err := k.Reencrypt(env, ctx)
	require.NoError(t, err)
	require.True(t, changed)
	id, _ := KeyID(re)
	require.Equal(t, k.CurrentKeyID(), id)
	require.NotEqual(t, oldID, id)

	// new ciphertext decrypts with the new key alone; old key no longer needed
	only := ring(t, keyA)
	pt, err = only.Decrypt(re, ctx)
	require.NoError(t, err)
	require.Equal(t, "payload", string(pt))

	// idempotent
	re2, changed, err := k.Reencrypt(re, ctx)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, re, re2)

	// wrong context is not laundered by rotation
	_, _, err = k.Reencrypt(env, "user:9:other")
	require.ErrorIs(t, err, ErrDecrypt)

	// without the old key the legacy ciphertext is unreadable
	_, err = only.Decrypt(env, ctx)
	require.ErrorIs(t, err, ErrUnknownKey)
}

func TestNonceUniqueness(t *testing.T) {
	k := ring(t, keyA)
	seen := map[string]bool{}
	for i := 0; i < 5000; i++ {
		env, err := k.Encrypt([]byte("same"), ctx)
		require.NoError(t, err)
		blob, _ := base64.StdEncoding.DecodeString(strings.Split(env, ":")[2])
		n := string(blob[:12])
		require.False(t, seen[n], "nonce reuse")
		seen[n] = true
	}
}

func TestMissingKey(t *testing.T) {
	_, err := NewKeyring("")
	require.ErrorIs(t, err, ErrNoKey)
	_, err = ParseKeyring("  ", "")
	require.ErrorIs(t, err, ErrNoKey)
	var k *Keyring
	_, err = k.Encrypt([]byte("x"), ctx)
	require.ErrorIs(t, err, ErrNoKey)
	_, err = (&Keyring{}).Decrypt("v1:aaaaaaaa:AAAA", ctx)
	require.ErrorIs(t, err, ErrNoKey)
}

func TestNoSecretLeaks(t *testing.T) {
	_, err := NewKeyring(keyA, "tooshort")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "tooshort")
	_, err = NewKeyring("zz!!short")
	require.NotContains(t, err.Error(), "zz!!short")

	k := ring(t, keyB, keyA)
	for _, s := range []string{fmt.Sprintf("%v", k), fmt.Sprintf("%+v", k), fmt.Sprintf("%#v", k), k.String()} {
		require.NotContains(t, s, keyB)
		require.NotContains(t, s, keyA)
		require.Contains(t, s, k.CurrentKeyID())
	}
	env, _ := k.Encrypt([]byte("plain-secret"), ctx)
	_, err = k.Decrypt(env, "other")
	require.NotContains(t, err.Error(), "plain-secret")
	require.NotContains(t, err.Error(), keyB)
}
