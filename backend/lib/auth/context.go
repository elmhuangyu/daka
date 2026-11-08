package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
)

// DefaultUserID is the identity used for every request in single-user mode.
const DefaultUserID = "default_user"

// User is the authenticated identity attached to a request context.
type User struct {
	ID string
}

// DefaultUser is the single-user mode identity.
var DefaultUser = User{ID: DefaultUserID}

// OIDCUserID derives the stable, collision-free user ID of an OIDC identity.
// The identity is the pair (issuer, subject); email is never part of it. The
// ID is "oidc_" plus the unpadded base64url SHA-256 of "iss|sub" (48 bytes, so
// it fits users.id) and can never equal DefaultUserID.
func OIDCUserID(issuer, subject string) string {
	return OIDCUserIDFromKey(issuer + "|" + subject)
}

// OIDCUserIDFromKey is OIDCUserID for an already joined "iss|sub" key, as typed
// on the `daka user claim --to` command line.
func OIDCUserIDFromKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "oidc_" + base64.RawURLEncoding.EncodeToString(sum[:])
}

type ctxKey struct{}
type sessionCtxKey struct{}

// WithUser returns a context carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom returns the user stored in ctx, if any.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// WithSession returns a context carrying the cookie session of the request.
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, s)
}

// SessionFrom returns the cookie session, absent in single-user mode without
// shared_secret.
func SessionFrom(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionCtxKey{}).(Session)
	return s, ok
}
