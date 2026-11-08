package auth

import "context"

// DefaultUserID is the identity used for every request in single-user mode.
const DefaultUserID = "default_user"

// User is the authenticated identity attached to a request context.
type User struct {
	ID string
}

// DefaultUser is the single-user mode identity.
var DefaultUser = User{ID: DefaultUserID}

type ctxKey struct{}

// WithUser returns a context carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom returns the user stored in ctx, if any.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}
