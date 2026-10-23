package main

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testProject = "cinefund-test"

type testSigner struct {
	kid string
	key *rsa.PrivateKey
}

func newTestSigner(t *testing.T, kid string) *testSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &testSigner{kid: kid, key: key}
}

// certPEM is the self-signed certificate Google's endpoint would publish for this key.
func (s *testSigner) certPEM(t *testing.T) string {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "securetoken-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &s.key.PublicKey, s.key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func b64(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// sign builds a compact JWT signed with RS256 using this signer's key.
func (s *testSigner) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	signingInput := b64(map[string]any{"alg": "RS256", "kid": s.kid, "typ": "JWT"}) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// keyServer fakes Google's securetoken certificate endpoint.
type keyServer struct {
	*httptest.Server
	mu     sync.Mutex
	certs  map[string]string
	hits   atomic.Int32
	status int
	maxAge int
}

func newKeyServer(t *testing.T, signers ...*testSigner) *keyServer {
	t.Helper()
	ks := &keyServer{certs: map[string]string{}, status: http.StatusOK, maxAge: 3600}
	for _, s := range signers {
		ks.certs[s.kid] = s.certPEM(t)
	}
	ks.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ks.hits.Add(1)
		ks.mu.Lock()
		defer ks.mu.Unlock()
		if ks.status != http.StatusOK {
			w.WriteHeader(ks.status)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(ks.maxAge)+", must-revalidate")
		_ = json.NewEncoder(w).Encode(ks.certs)
	}))
	t.Cleanup(ks.Close)
	return ks
}

func (ks *keyServer) publish(t *testing.T, s *testSigner) {
	t.Helper()
	ks.mu.Lock()
	ks.certs[s.kid] = s.certPEM(t)
	ks.mu.Unlock()
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestVerifier(ks *keyServer, c *clock) *FirebaseVerifier {
	return &FirebaseVerifier{
		projectID: testProject,
		keysURL:   ks.URL,
		client:    ks.Client(),
		now:       c.now,
	}
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss":            "https://securetoken.google.com/" + testProject,
		"aud":            testProject,
		"sub":            "uid-123",
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Add(-time.Minute).Unix(),
		"auth_time":      now.Add(-time.Minute).Unix(),
		"email":          "ava@example.com",
		"email_verified": true,
		"name":           "Ava Chen",
		"picture":        "https://example.com/a.png",
	}
}

func TestFirebaseVerifier_ValidToken(t *testing.T) {
	signer := newTestSigner(t, "kid-1")
	ks := newKeyServer(t, signer)
	clk := &clock{t: time.Now()}
	v := newTestVerifier(ks, clk)

	info, err := v.Verify(context.Background(), signer.sign(t, validClaims(clk.t)))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if info.Subject != "uid-123" || info.Email != "ava@example.com" || info.EmailVerified != "true" || info.Name != "Ava Chen" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestFirebaseVerifier_RejectsBadClaims(t *testing.T) {
	signer := newTestSigner(t, "kid-1")
	ks := newKeyServer(t, signer)
	clk := &clock{t: time.Now()}
	v := newTestVerifier(ks, clk)

	cases := map[string]struct {
		mutate func(map[string]any)
		want   string
	}{
		"expired":        {func(c map[string]any) { c["exp"] = clk.t.Add(-time.Second).Unix() }, "expired"},
		"no exp":         {func(c map[string]any) { delete(c, "exp") }, "expired"},
		"iat future":     {func(c map[string]any) { c["iat"] = clk.t.Add(time.Hour).Unix() }, "future"},
		"auth_time next": {func(c map[string]any) { c["auth_time"] = clk.t.Add(time.Hour).Unix() }, "future"},
		"wrong audience": {func(c map[string]any) { c["aud"] = "someone-elses-project" }, "audience"},
		"wrong issuer":   {func(c map[string]any) { c["iss"] = "https://accounts.google.com" }, "issuer"},
		"empty subject":  {func(c map[string]any) { c["sub"] = "" }, "subject"},
		"long subject":   {func(c map[string]any) { c["sub"] = strings.Repeat("x", 129) }, "subject"},
	}
	for name, tc := range cases {
		claims := validClaims(clk.t)
		tc.mutate(claims)
		_, err := v.Verify(context.Background(), signer.sign(t, claims))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want error containing %q, got %v", name, tc.want, err)
		}
	}
}

func TestFirebaseVerifier_RejectsForgeries(t *testing.T) {
	signer := newTestSigner(t, "kid-1")
	attacker := newTestSigner(t, "kid-1") // same kid, different key
	ks := newKeyServer(t, signer)
	clk := &clock{t: time.Now()}
	v := newTestVerifier(ks, clk)
	claims := validClaims(clk.t)

	// Right kid, wrong private key.
	if _, err := v.Verify(context.Background(), attacker.sign(t, claims)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("wrong signer: got %v", err)
	}

	// alg=none with no signature.
	none := b64(map[string]any{"alg": "none", "kid": "kid-1"}) + "." + b64(claims) + "."
	if _, err := v.Verify(context.Background(), none); err == nil {
		t.Error("alg=none accepted")
	}

	// HS256 using the published certificate as the HMAC secret (algorithm confusion).
	signingInput := b64(map[string]any{"alg": "HS256", "kid": "kid-1"}) + "." + b64(claims)
	mac := hmac.New(sha256.New, []byte(signer.certPEM(t)))
	mac.Write([]byte(signingInput))
	hs := signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := v.Verify(context.Background(), hs); err == nil || !strings.Contains(err.Error(), "algorithm") {
		t.Errorf("HS256 confusion: got %v", err)
	}

	// A valid token with its payload swapped for different claims.
	parts := strings.Split(signer.sign(t, claims), ".")
	claims["sub"] = "admin"
	tampered := parts[0] + "." + b64(claims) + "." + parts[2]
	if _, err := v.Verify(context.Background(), tampered); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("tampered payload: got %v", err)
	}

	for _, bad := range []string{"", "   ", "a.b", "a.b.c.d", "not-a-jwt", "@@@.@@@.@@@"} {
		if _, err := v.Verify(context.Background(), bad); err == nil {
			t.Errorf("malformed token %q accepted", bad)
		}
	}
}

func TestFirebaseVerifier_CachesKeys(t *testing.T) {
	signer := newTestSigner(t, "kid-1")
	ks := newKeyServer(t, signer)
	clk := &clock{t: time.Now()}
	v := newTestVerifier(ks, clk)

	for i := 0; i < 5; i++ {
		if _, err := v.Verify(context.Background(), signer.sign(t, validClaims(clk.t))); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	if got := ks.hits.Load(); got != 1 {
		t.Fatalf("key endpoint hit %d times for 5 verifications, want 1", got)
	}

	// Past max-age the set is fetched again.
	clk.t = clk.t.Add(2 * time.Hour)
	if _, err := v.Verify(context.Background(), signer.sign(t, validClaims(clk.t))); err != nil {
		t.Fatalf("verify after expiry: %v", err)
	}
	if got := ks.hits.Load(); got != 2 {
		t.Fatalf("key endpoint hit %d times after TTL, want 2", got)
	}
}

// Forged key ids must not become a stream of requests to Google, but a real
// rotation (new kid) must be picked up without waiting for the TTL.
func TestFirebaseVerifier_UnknownKidRefreshIsRateLimited(t *testing.T) {
	old := newTestSigner(t, "kid-old")
	ks := newKeyServer(t, old)
	clk := &clock{t: time.Now()}
	v := newTestVerifier(ks, clk)

	if _, err := v.Verify(context.Background(), old.sign(t, validClaims(clk.t))); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	for i := 0; i < 20; i++ {
		forged := newTestSigner(t, "forged-"+string(rune('a'+i)))
		if _, err := v.Verify(context.Background(), forged.sign(t, validClaims(clk.t))); err == nil || !strings.Contains(err.Error(), "unknown key") {
			t.Fatalf("forged kid %d: got %v", i, err)
		}
	}
	if got := ks.hits.Load(); got != 1 {
		t.Fatalf("20 forged kids caused %d fetches, want the single initial one", got)
	}

	// Google rotates: a new key appears and the minute has passed.
	rotated := newTestSigner(t, "kid-new")
	ks.publish(t, rotated)
	clk.t = clk.t.Add(2 * time.Minute)
	if _, err := v.Verify(context.Background(), rotated.sign(t, validClaims(clk.t))); err != nil {
		t.Fatalf("rotated key not picked up: %v", err)
	}
}

func TestFirebaseVerifier_FailsClosedWhenKeysUnavailable(t *testing.T) {
	signer := newTestSigner(t, "kid-1")
	ks := newKeyServer(t, signer)
	ks.status = http.StatusInternalServerError
	clk := &clock{t: time.Now()}
	v := newTestVerifier(ks, clk)

	if _, err := v.Verify(context.Background(), signer.sign(t, validClaims(clk.t))); err == nil {
		t.Fatal("token accepted although signing keys could not be fetched")
	}
}

func TestKeyTTL(t *testing.T) {
	cases := map[string]time.Duration{
		"public, max-age=19500, must-revalidate": 19500 * time.Second,
		"max-age=5":                              time.Minute, // floor
		"max-age=9999999":                        24 * time.Hour,
		"":                                       time.Hour, // default
		"no-store":                               time.Hour,
	}
	for header, want := range cases {
		if got := keyTTL(header); got != want {
			t.Errorf("keyTTL(%q) = %v, want %v", header, got, want)
		}
	}
}
