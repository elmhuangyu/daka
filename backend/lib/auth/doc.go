// Package auth provides request authentication for the three deployment modes
// (DESIGN 8):
//
//   - single-user, no shared_secret: every request is default_user and the
//     only protection is the loopback bind. To stop DNS rebinding (an attacker
//     page whose name resolves to 127.0.0.1) HostGuard refuses, for all
//     methods, any Host that is not localhost, a loopback IP, the listen host
//     or in server.allowed_hosts. On top of that, state-changing requests that
//     carry Origin / Sec-Fetch-Site must be same-origin. This is browser-CSRF
//     hygiene, not authentication: any local process can still call the API.
//   - single-user with shared_secret: POST /api/auth/login creates a server-side
//     session (cookie daka_session).
//   - OIDC: the backend is the BFF. Authorization Code + PKCE (S256); the ID
//     token is verified (JWKS signature, iss, aud, exp, nonce) and then
//     discarded together with the access / refresh tokens, which are never
//     stored. The user identity is iss + sub (see OIDCUserID), never the email.
//
// Sessions are database rows keyed by the SHA-256 of a 256-bit random cookie.
// Cookies are httpOnly, SameSite=Lax (Strict would break the IdP redirect back)
// and Secure per auth.secure_cookie. State-changing requests authenticated by
// a session need the X-Daka-CSRF header holding the session-bound token served
// by GET /api/auth/me.
package auth
