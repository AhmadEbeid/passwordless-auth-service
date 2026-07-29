package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

// Google's documented ID-token issuer claim.
var googleIssuers = map[string]bool{
	"https://accounts.google.com": true,
	"accounts.google.com":         true,
}

// GoogleIdentityVerifier is the real IdentityVerifier adapter: it checks a
// Google ID token's signature against Google's published JWKS and validates
// issuer/audience/expiry — no client secret needed, only the OAuth client ID
// the token must be issued for.
//
// It is structured to Google's documented ID-token verification contract but
// has not been exercised against a live token: doing so needs a real
// GOOGLE_CLIENT_ID and a token minted by Google Sign-In, neither available in
// this environment (deferred to ops, mirroring the WhatsApp sender adapter).
type GoogleIdentityVerifier struct {
	clientID   string
	httpClient *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// NewGoogleIdentityVerifier builds the adapter for the given OAuth client ID
// (the required `aud` claim on every token it accepts).
func NewGoogleIdentityVerifier(clientID string) *GoogleIdentityVerifier {
	return &GoogleIdentityVerifier{
		clientID:   clientID,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (v *GoogleIdentityVerifier) Verify(ctx context.Context, idToken string) (*GoogleIdentity, error) {
	token, err := jwt.Parse(idToken, v.keyFunc(ctx), jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: %w", ErrGoogleFailed, err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrGoogleFailed
	}
	iss, _ := claims["iss"].(string)
	aud, _ := claims["aud"].(string)
	sub, _ := claims["sub"].(string)
	if !googleIssuers[iss] || v.clientID == "" || aud != v.clientID || sub == "" {
		return nil, ErrGoogleFailed
	}

	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	return &GoogleIdentity{Subject: sub, Email: email, EmailVerified: emailVerified}, nil
}

func (v *GoogleIdentityVerifier) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("auth: google token missing kid")
		}
		return v.publicKey(ctx, kid)
	}
}

// publicKey returns the cached signing key for kid, refreshing Google's JWKS
// (at most once an hour) when the key is unknown.
func (v *GoogleIdentityVerifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if key, ok := v.keys[kid]; ok && time.Since(v.fetchedAt) < time.Hour {
		return key, nil
	}
	if err := v.refreshKeysLocked(ctx); err != nil {
		return nil, fmt.Errorf("auth: refresh google jwks: %w", err)
	}
	key, ok := v.keys[kid]
	if !ok {
		return nil, fmt.Errorf("auth: unknown google signing key %q", kid)
	}
	return key, nil
}

type googleJWKS struct {
	Keys []struct {
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// refreshKeysLocked fetches Google's current signing keys. Callers hold mu.
func (v *GoogleIdentityVerifier) refreshKeysLocked(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleCertsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var jwks googleJWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	v.keys = keys
	v.fetchedAt = time.Now()
	return nil
}

func rsaPublicKeyFromJWK(nEncoded, eEncoded string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nEncoded)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eEncoded)
	if err != nil {
		return nil, err
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

// pendingGoogleLinkTTL bounds how long a verified-but-not-yet-linked Google
// identity may be redeemed via RequestVerification — the same window as the
// OTP challenge it feeds into.
const pendingGoogleLinkTTL = OTPTTL

type pendingGoogleLinkClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
}

// signPendingGoogleLink mints a server-signed, short-lived token carrying an
// already-verified Google identity (subject/email). This is what
// GoogleExchange's no-account branch hands the client instead of the raw
// identity — the client cannot forge or alter it (HMAC over a server-only
// secret), which is what makes it safe for RequestVerification to trust later:
// a client can never link an account to a Google identity it did not just
// prove ownership of via a real ID-token exchange.
func signPendingGoogleLink(secret []byte, identity *GoogleIdentity, now time.Time) (string, error) {
	claims := pendingGoogleLinkClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   identity.Subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(pendingGoogleLinkTTL)),
		},
		Email: identity.Email,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign pending google link: %w", err)
	}
	return signed, nil
}

// verifyPendingGoogleLink checks a token minted by signPendingGoogleLink,
// returning the verified subject/email it carries. now drives expiry
// validation, read from the injected Clock so this stays deterministic.
func verifyPendingGoogleLink(secret []byte, tokenString string, now time.Time) (subject, email string, err error) {
	var claims pendingGoogleLinkClaims
	_, err = jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrGoogleFailed, err)
	}
	return claims.Subject, claims.Email, nil
}
