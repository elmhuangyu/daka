// Package secret encrypts small secrets at rest (e.g. external calendar URLs)
// with AES-256-GCM under a master key from DAKA_SECRET_KEY.
//
// Envelope: "v1:<key_id>:<base64(nonce||ciphertext)>" (standard base64). The
// key_id is the first 8 hex characters of SHA-256("daka/secret/key-id" || key);
// it identifies which key encrypted the value so keys can be rotated. The
// caller-provided context string (for example "user:42:external_calendar") is
// bound as GCM additional data, so a ciphertext cannot be moved to another row.
//
// Master key formats (DAKA_SECRET_KEY, and each comma-separated entry of
// DAKA_SECRET_KEY_OLD), tried in this order:
//  1. 64 hex characters (32 raw bytes);
//  2. base64 (standard or URL-safe, padded or not) decoding to exactly 32 bytes;
//  3. otherwise a passphrase of at least 16 characters, stretched to 32 bytes
//     with HKDF-SHA256. HKDF is NOT a password-hardening KDF: use a random
//     key (e.g. `openssl rand -base64 32`) rather than a human-chosen phrase.
//
// Error values never contain key material or plaintext.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Errors returned by this package.
var (
	ErrNoKey           = errors.New("secret: no master key configured (set DAKA_SECRET_KEY)")
	ErrInvalidKey      = errors.New("secret: invalid master key (want 64 hex chars, base64 of 32 bytes, or a passphrase of at least 16 characters)")
	ErrInvalidEnvelope = errors.New("secret: malformed ciphertext envelope")
	ErrUnknownKey      = errors.New("secret: ciphertext was encrypted with an unknown key id")
	ErrDecrypt         = errors.New("secret: decryption failed (wrong key, wrong context or tampered data)")
)

const (
	version        = "v1"
	keyLen         = 32
	minPassphrase  = 16
	keyIDLen       = 8
	hkdfSalt       = "daka/secret/hkdf-salt/v1"
	hkdfInfo       = "daka/secret/master-key/v1"
	keyIDDomain    = "daka/secret/key-id"
	minEnvelopeLen = 12 + 16 // nonce + GCM tag
)

type key struct {
	id  string
	gcm cipher.AEAD
}

// Keyring holds the current encryption key and optional retired keys that can
// still decrypt. The zero value and nil have no key: Encrypt/Decrypt return
// ErrNoKey. A Keyring is immutable and safe for concurrent use.
type Keyring struct {
	current *key
	byID    map[string]*key
}

// NewKeyring builds a Keyring from the current key and retired keys, each in
// one of the supported formats. An empty current yields ErrNoKey; blank retired
// entries are ignored.
func NewKeyring(current string, retired ...string) (*Keyring, error) {
	if strings.TrimSpace(current) == "" {
		return nil, ErrNoKey
	}
	cur, err := parseKey(current)
	if err != nil {
		return nil, fmt.Errorf("DAKA_SECRET_KEY: %w", err)
	}
	k := &Keyring{current: cur, byID: map[string]*key{cur.id: cur}}
	for i, r := range retired {
		if strings.TrimSpace(r) == "" {
			continue
		}
		o, err := parseKey(r)
		if err != nil {
			return nil, fmt.Errorf("DAKA_SECRET_KEY_OLD entry %d: %w", i+1, err)
		}
		if _, dup := k.byID[o.id]; !dup {
			k.byID[o.id] = o
		}
	}
	return k, nil
}

// ParseKeyring builds a Keyring from the DAKA_SECRET_KEY value and the
// comma-separated DAKA_SECRET_KEY_OLD value.
func ParseKeyring(current, retiredCSV string) (*Keyring, error) {
	var old []string
	if strings.TrimSpace(retiredCSV) != "" {
		old = strings.Split(retiredCSV, ",")
	}
	return NewKeyring(current, old...)
}

// CurrentKeyID returns the id used for new ciphertexts ("" without a key).
func (k *Keyring) CurrentKeyID() string {
	if k == nil || k.current == nil {
		return ""
	}
	return k.current.id
}

// String never reveals key material (only key ids).
func (k *Keyring) String() string {
	if k == nil || k.current == nil {
		return "Keyring{empty}"
	}
	return fmt.Sprintf("Keyring{current=%s keys=%d}", k.current.id, len(k.byID))
}

// GoString implements fmt.GoStringer.
func (k *Keyring) GoString() string { return k.String() }

func parseKey(s string) (*key, error) {
	s = strings.TrimSpace(s)
	var raw []byte
	if b, err := hex.DecodeString(s); err == nil && len(b) == keyLen {
		raw = b
	} else if b := decodeBase64(s); len(b) == keyLen {
		raw = b
	} else if len(s) >= minPassphrase {
		d, err := hkdf.Key(sha256.New, []byte(s), []byte(hkdfSalt), hkdfInfo, keyLen)
		if err != nil {
			return nil, ErrInvalidKey
		}
		raw = d
	} else {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, ErrInvalidKey
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalidKey
	}
	h := sha256.Sum256(append([]byte(keyIDDomain), raw...))
	return &key{id: hex.EncodeToString(h[:])[:keyIDLen], gcm: gcm}, nil
}

func decodeBase64(s string) []byte {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b
		}
	}
	return nil
}

// Encrypt seals plaintext under the current key, binding context as AAD.
func (k *Keyring) Encrypt(plaintext []byte, context string) (string, error) {
	if k == nil || k.current == nil {
		return "", ErrNoKey
	}
	return seal(k.current, plaintext, context)
}

func seal(key *key, plaintext []byte, context string) (string, error) {
	nonce := make([]byte, key.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("secret: random source failure")
	}
	out := key.gcm.Seal(nonce, nonce, plaintext, []byte(context))
	return version + ":" + key.id + ":" + base64.StdEncoding.EncodeToString(out), nil
}

// KeyID extracts the key id of an envelope without decrypting it.
func KeyID(envelope string) (string, error) {
	id, _, err := split(envelope)
	return id, err
}

func split(envelope string) (id string, blob []byte, err error) {
	parts := strings.Split(envelope, ":")
	if len(parts) != 3 || parts[0] != version || len(parts[1]) != keyIDLen {
		return "", nil, ErrInvalidEnvelope
	}
	blob, derr := base64.StdEncoding.DecodeString(parts[2])
	if derr != nil || len(blob) < minEnvelopeLen {
		return "", nil, ErrInvalidEnvelope
	}
	return parts[1], blob, nil
}

// Decrypt opens an envelope with the key named by its key id.
func (k *Keyring) Decrypt(envelope, context string) ([]byte, error) {
	if k == nil || k.current == nil {
		return nil, ErrNoKey
	}
	id, blob, err := split(envelope)
	if err != nil {
		return nil, err
	}
	key, ok := k.byID[id]
	if !ok {
		return nil, ErrUnknownKey
	}
	ns := key.gcm.NonceSize()
	pt, err := key.gcm.Open(nil, blob[:ns], blob[ns:], []byte(context))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// Reencrypt decrypts envelope and, if it was not written with the current key,
// re-seals it under the current key. changed reports whether the returned
// envelope differs (callers persist it only then). A value already under the
// current key is still authenticated but returned unchanged.
func (k *Keyring) Reencrypt(envelope, context string) (out string, changed bool, err error) {
	pt, err := k.Decrypt(envelope, context)
	if err != nil {
		return "", false, err
	}
	id, _, _ := split(envelope)
	if id == k.current.id {
		return envelope, false, nil
	}
	out, err = seal(k.current, pt, context)
	if err != nil {
		return "", false, err
	}
	return out, true, nil
}
