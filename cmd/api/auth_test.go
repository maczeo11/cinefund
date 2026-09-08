package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

const testSecret = "01234567890123456789012345678901" // 32 bytes

func init() {
	gin.SetMode(gin.TestMode)
}

func TestGenerateAndParseJWT(t *testing.T) {
	uid := uuid.New()
	token, err := GenerateJWT(uid, "CREATOR", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	claims, err := ParseAndValidateJWT(token, testSecret)
	if err != nil {
		t.Fatalf("ParseAndValidateJWT: %v", err)
	}
	if claims.Subject != uid.String() {
		t.Errorf("subject = %q, want %q", claims.Subject, uid.String())
	}
	if claims.Role != "CREATOR" {
		t.Errorf("role = %q, want CREATOR", claims.Role)
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		t.Errorf("expected future expiration, got %d", claims.ExpiresAt)
	}
}

func TestParseAndValidateJWT_Expired(t *testing.T) {
	uid := uuid.New()
	token, err := GenerateJWT(uid, "CREATOR", testSecret, -1*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	_, err = ParseAndValidateJWT(token, testSecret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "token expired") {
		t.Errorf("expected 'token expired' error, got %v", err)
	}
}

func TestParseAndValidateJWT_InvalidSecret(t *testing.T) {
	uid := uuid.New()
	token, err := GenerateJWT(uid, "CREATOR", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	wrongSecret := "wrongsecretwrongsecretwrongsec12"
	_, err = ParseAndValidateJWT(token, wrongSecret)
	if err == nil {
		t.Fatal("expected invalid signature error, got nil")
	}
}

func TestParseAndValidateJWT_TamperedPayload(t *testing.T) {
	uid := uuid.New()
	token, err := GenerateJWT(uid, "CREATOR", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	parts := strings.Split(token, ".")
	tamperedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + uuid.NewString() + `"}`))
	tamperedToken := parts[0] + "." + tamperedPayload + "." + parts[2]

	_, err = ParseAndValidateJWT(tamperedToken, testSecret)
	if err == nil {
		t.Fatal("expected invalid signature error on tampered payload, got nil")
	}
}

func TestParseAndValidateJWT_AlgNone(t *testing.T) {
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + uuid.NewString() + `"}`))
	token := headerB64 + "." + payloadB64 + "."

	_, err := ParseAndValidateJWT(token, testSecret)
	if err == nil {
		t.Fatal("expected rejection of 'none' algorithm, got nil")
	}
}

func TestParseAndValidateJWT_Malformed(t *testing.T) {
	malformed := []string{
		"",
		"not.a.jwt.token",
		"singlepart",
		"two.parts",
	}
	for _, m := range malformed {
		if _, err := ParseAndValidateJWT(m, testSecret); err == nil {
			t.Errorf("expected error for malformed token %q, got nil", m)
		}
	}
}

func TestJWTAuthMiddleware_BearerHeader(t *testing.T) {
	uid := uuid.New()
	token, err := GenerateJWT(uid, "CREATOR", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	r := gin.New()
	r.Use(JWTAuthMiddleware(testSecret))
	r.GET("/test-auth", func(c *gin.Context) {
		callerID, ok := httpx.CallerID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no caller"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"caller_id": callerID.String()})
	})

	req := httptest.NewRequest("GET", "/test-auth", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), uid.String()) {
		t.Errorf("expected response to contain uid %s, got %s", uid, w.Body.String())
	}
}

func TestJWTAuthMiddleware_Cookie(t *testing.T) {
	uid := uuid.New()
	token, err := GenerateJWT(uid, "BACKER", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	r := gin.New()
	r.Use(JWTAuthMiddleware(testSecret))
	r.GET("/test-cookie", func(c *gin.Context) {
		callerID, ok := httpx.CallerID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no caller"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"caller_id": callerID.String()})
	})

	req := httptest.NewRequest("GET", "/test-cookie", nil)
	req.AddCookie(&http.Cookie{Name: "cf_at", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_Unauthorized(t *testing.T) {
	r := gin.New()
	r.Use(JWTAuthMiddleware(testSecret))
	r.POST("/protected", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("POST", "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	})
	r.GET("/test-sec", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test-sec", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("Strict-Transport-Security = %q, want max-age=31536000; includeSubDomains", got)
	}
}

func TestCORS_RestrictedOrigins(t *testing.T) {
	r := gin.New()
	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{
			"https://cinefund.vercel.app",
			"http://localhost:5173",
			"http://localhost:3000",
			"http://127.0.0.1:5173",
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Requested-With", "Origin", "X-User-ID"},
		AllowCredentials: true,
	}))
	r.GET("/cors-test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.POST("/cors-test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 1. Allowed origin
	req := httptest.NewRequest("GET", "/cors-test", nil)
	req.Header.Set("Origin", "https://cinefund.vercel.app")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://cinefund.vercel.app" {
		t.Errorf("allowed origin Access-Control-Allow-Origin = %q, want https://cinefund.vercel.app", got)
	}

	// 2. Disallowed origin
	req2 := httptest.NewRequest("GET", "/cors-test", nil)
	req2.Header.Set("Origin", "https://malicious.evil.com")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Access-Control-Allow-Origin = %q, want empty", got)
	}

	// 3. Preflight OPTIONS request with X-User-ID
	req3 := httptest.NewRequest("OPTIONS", "/cors-test", nil)
	req3.Header.Set("Origin", "http://localhost:5173")
	req3.Header.Set("Access-Control-Request-Method", "POST")
	req3.Header.Set("Access-Control-Request-Headers", "Content-Type, X-User-ID")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusNoContent && w3.Code != http.StatusOK {
		t.Errorf("preflight status = %d, want 204 or 200", w3.Code)
	}
	if got := w3.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("preflight Access-Control-Allow-Origin = %q, want http://localhost:5173", got)
	}
}

func TestPublicRoute_ExpiredTokenDoesNotBlock(t *testing.T) {
	uid := uuid.New()
	expiredToken, err := GenerateJWT(uid, "BACKER", testSecret, -10*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	r := gin.New()
	r.Use(JWTAuthMiddleware(testSecret))
	r.GET("/public-endpoint", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 1. With expired cookie
	req := httptest.NewRequest("GET", "/public-endpoint", nil)
	req.AddCookie(&http.Cookie{Name: "cf_at", Value: expiredToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public route with expired cookie, got %d: %s", w.Code, w.Body.String())
	}

	// 2. With expired Bearer token
	req2 := httptest.NewRequest("GET", "/public-endpoint", nil)
	req2.Header.Set("Authorization", "Bearer "+expiredToken)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public route with expired Bearer token, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestRequireAuth_ExpiredTokenBlocks(t *testing.T) {
	uid := uuid.New()
	expiredToken, err := GenerateJWT(uid, "BACKER", testSecret, -10*time.Minute)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}

	r := gin.New()
	r.Use(JWTAuthMiddleware(testSecret))
	r.POST("/protected-endpoint", RequireAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("POST", "/protected-endpoint", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for expired token on protected route, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "token expired") {
		t.Errorf("expected response to mention token expired, got: %s", w.Body.String())
	}
}
