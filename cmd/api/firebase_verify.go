package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// firebaseKeysURL serves the public certificates (kid -> PEM) that Firebase
// signs ID tokens with.
const firebaseKeysURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

const (
	// clockLeeway absorbs small clock differences between Google and us when
	// checking iat / auth_time.
	clockLeeway = 60 * time.Second
	// minKeyRefresh bounds how often an unknown key id can trigger a re-fetch,
	// so a stream of forged tokens cannot turn into a stream of requests to Google.
	minKeyRefresh = time.Minute
	defaultKeyTTL = time.Hour
)

// FirebaseTokenInfo holds the identity claims we use from a verified Firebase
// ID token.
type FirebaseTokenInfo struct {
	Subject       string
	Email         string
	EmailVerified string // "true" or "false"
	Name          string
	Picture       string
}

// idTokenVerifier turns a client-supplied Firebase ID token into verified claims.
type idTokenVerifier interface {
	Verify(ctx context.Context, idToken string) (*FirebaseTokenInfo, error)
}

type firebaseClaims struct {
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	ExpiresAt     int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
	AuthTime      int64  `json:"auth_time"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// FirebaseVerifier verifies Firebase Auth ID tokens locally, as described in
// https://firebase.google.com/docs/auth/admin/verify-id-tokens: an RS256
// signature checked against Google's published keys, plus the aud, iss, exp,
// iat, sub and auth_time claims.
//
// Google's tokeninfo endpoint is deliberately not used. It validates Google
// OAuth tokens, not Firebase ID tokens, and answers 400 "Invalid Value" for them.
type FirebaseVerifier struct {
	projectID string
	keysURL   string
	client    *http.Client
	now       func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
	fetchedAt time.Time
}

// NewFirebaseVerifier verifies tokens issued for the given Firebase project.
func NewFirebaseVerifier(projectID string) *FirebaseVerifier {
	return &FirebaseVerifier{
		projectID: projectID,
		keysURL:   firebaseKeysURL,
		client:    &http.Client{Timeout: 6 * time.Second},
		now:       time.Now,
	}
}

// Verify checks idToken and returns its claims.
func (v *FirebaseVerifier) Verify(ctx context.Context, idToken string) (*FirebaseTokenInfo, error) {
	token := strings.TrimSpace(idToken)
	if token == "" {
		return nil, errors.New("empty id_token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return nil, errors.New("malformed token header")
	}
	// Pin the algorithm: accepting whatever the token claims is how "none" and
	// HMAC-with-the-public-key forgeries work.
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported algorithm %q", header.Alg)
	}
	if header.Kid == "" {
		return nil, errors.New("token has no key id")
	}

	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return nil, err
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("malformed token signature")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errors.New("invalid token signature")
	}

	var claims firebaseClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return nil, errors.New("malformed token payload")
	}
	if err := v.checkClaims(&claims); err != nil {
		return nil, err
	}

	return &FirebaseTokenInfo{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: strconv.FormatBool(claims.EmailVerified),
		Name:          claims.Name,
		Picture:       claims.Picture,
	}, nil
}

func (v *FirebaseVerifier) checkClaims(c *firebaseClaims) error {
	now := v.now()
	if c.ExpiresAt == 0 || !now.Before(time.Unix(c.ExpiresAt, 0)) {
		return errors.New("token expired")
	}
	if time.Unix(c.IssuedAt, 0).After(now.Add(clockLeeway)) {
		return errors.New("token issued in the future")
	}
	if time.Unix(c.AuthTime, 0).After(now.Add(clockLeeway)) {
		return errors.New("token auth_time is in the future")
	}
	if c.Audience != v.projectID {
		return errors.New("token audience mismatch")
	}
	if c.Issuer != "https://securetoken.google.com/"+v.projectID {
		return errors.New("token issuer mismatch")
	}
	if c.Subject == "" || len(c.Subject) > 128 {
		return errors.New("token has no valid subject")
	}
	return nil
}

// key returns the public key for kid, refreshing the cached set when it has
// expired or (at most once a minute) when the kid is unknown, which is what a
// key rotation looks like.
func (v *FirebaseVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.now()
	if k, ok := v.keys[kid]; ok && now.Before(v.expiresAt) {
		return k, nil
	}
	if v.keys == nil || !now.Before(v.expiresAt) || now.Sub(v.fetchedAt) >= minKeyRefresh {
		if err := v.refreshLocked(ctx, now); err != nil {
			// Fail closed, but keep serving keys we already hold if Google is briefly unreachable.
			if k, ok := v.keys[kid]; ok && now.Before(v.expiresAt) {
				return k, nil
			}
			return nil, err
		}
	}
	k, ok := v.keys[kid]
	if !ok {
		return nil, errors.New("token signed with an unknown key")
	}
	return k, nil
}

func (v *FirebaseVerifier) refreshLocked(ctx context.Context, now time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.keysURL, nil)
	if err != nil {
		return fmt.Errorf("build key request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch signing keys: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("fetch signing keys: status %d", resp.StatusCode)
	}

	var certs map[string]string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&certs); err != nil {
		return fmt.Errorf("decode signing keys: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(certs))
	for kid, certPEM := range certs {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			keys[kid] = pub
		}
	}
	if len(keys) == 0 {
		return errors.New("fetch signing keys: no usable keys in response")
	}

	v.keys = keys
	v.fetchedAt = now
	v.expiresAt = now.Add(keyTTL(resp.Header.Get("Cache-Control")))
	return nil
}

// keyTTL reads max-age from a Cache-Control header, bounded to [1m, 24h].
func keyTTL(cacheControl string) time.Duration {
	ttl := defaultKeyTTL
	for _, directive := range strings.Split(cacheControl, ",") {
		directive = strings.TrimSpace(directive)
		if rest, ok := strings.CutPrefix(directive, "max-age="); ok {
			if secs, err := strconv.Atoi(rest); err == nil {
				ttl = time.Duration(secs) * time.Second
			}
		}
	}
	return min(max(ttl, time.Minute), 24*time.Hour)
}

func decodeSegment(seg string, dst any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
