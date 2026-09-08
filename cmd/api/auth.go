package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
)

// JWTClaims holds parsed access token claims.
type JWTClaims struct {
	Subject   string `json:"sub"`
	UserID    string `json:"user_id,omitempty"`
	Role      string `json:"role,omitempty"`
	ExpiresAt int64  `json:"exp,omitempty"`
	IssuedAt  int64  `json:"iat,omitempty"`
}

// GenerateJWT creates a signed HS256 JWT access token.
func GenerateJWT(userID uuid.UUID, role, secret string, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("jwt secret cannot be empty")
	}
	now := time.Now()
	claims := JWTClaims{
		Subject:   userID.String(),
		UserID:    userID.String(),
		Role:      role,
		ExpiresAt: now.Add(ttl).Unix(),
		IssuedAt:  now.Unix(),
	}

	headerJSON := `{"alg":"HS256","typ":"JWT"}`
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	data := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return data + "." + sigB64, nil
}

// ParseAndValidateJWT verifies signature and expiration of an HS256 JWT.
func ParseAndValidateJWT(tokenStr, secret string) (*JWTClaims, error) {
	if len(secret) == 0 {
		return nil, errors.New("jwt secret cannot be empty")
	}
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format: expected 3 segments")
	}

	// 1. Header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		headerBytes, err = base64.URLEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, errors.New("invalid header encoding")
		}
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, errors.New("invalid header json")
	}
	if header.Alg != "HS256" {
		return nil, fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// 2. Signature verification
	data := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	expectedSig := mac.Sum(nil)

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		sigBytes, err = base64.URLEncoding.DecodeString(parts[2])
		if err != nil {
			return nil, errors.New("invalid signature encoding")
		}
	}
	if !hmac.Equal(expectedSig, sigBytes) {
		return nil, errors.New("invalid signature")
	}

	// 3. Payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payloadBytes, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, errors.New("invalid payload encoding")
		}
	}
	var claims JWTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, errors.New("invalid payload json")
	}

	if claims.ExpiresAt != 0 && time.Now().Unix() > claims.ExpiresAt {
		return nil, errors.New("token expired")
	}

	return &claims, nil
}

// JWTAuthMiddleware parses JWT from Bearer header or cf_at cookie, with X-User-ID fallback.
func JWTAuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := ""
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		} else if cookie, err := c.Cookie("cf_at"); err == nil && cookie != "" {
			tokenStr = cookie
		}

		if tokenStr != "" {
			claims, err := ParseAndValidateJWT(tokenStr, secret)
			if err != nil {
				c.Set("auth_error", err)
			} else {
				userIDStr := claims.Subject
				if userIDStr == "" {
					userIDStr = claims.UserID
				}
				if uid, err := uuid.Parse(userIDStr); err == nil && uid != uuid.Nil {
					httpx.SetCallerID(c, uid)
					c.Set(httpx.ClaimsKey, claims)
				}
			}
		}

		if _, ok := httpx.CallerID(c); !ok {
			if devUserID := c.GetHeader("X-User-ID"); devUserID != "" {
				if uid, err := uuid.Parse(devUserID); err == nil && uid != uuid.Nil {
					httpx.SetCallerID(c, uid)
				}
			}
		}

		c.Next()
	}
}

// RequireAuth ensures that the request has an authenticated caller.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := httpx.CallerID(c); !ok {
			if authErr, exists := c.Get("auth_error"); exists {
				httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "invalid authentication token: %v", authErr))
				return
			}
			httpx.Abort(c, errs.Unauthorized("UNAUTHORIZED", "authentication required"))
			return
		}
		c.Next()
	}
}
