package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/maczeo11/cinefund/internal/platform/errs"
	"github.com/maczeo11/cinefund/internal/platform/httpx"
	"github.com/maczeo11/cinefund/internal/platform/postgres"
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

// FirebaseTokenInfo holds claims returned by Google's tokeninfo endpoint.
type FirebaseTokenInfo struct {
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	ExpiresIn     string `json:"expires_in"`
}

// VerifyFirebaseIDToken calls Google's public tokeninfo endpoint to verify a Firebase ID token.
func VerifyFirebaseIDToken(ctx context.Context, idToken, expectedProjectID string) (*FirebaseTokenInfo, error) {
	cleanToken := strings.TrimSpace(idToken)
	if cleanToken == "" {
		return nil, errors.New("empty id_token")
	}

	reqURL := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(cleanToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create tokeninfo request: %w", err)
	}

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tokeninfo request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("invalid token (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var info FirebaseTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode tokeninfo: %w", err)
	}

	if expectedProjectID != "" && info.Audience != expectedProjectID {
		return nil, fmt.Errorf("token audience mismatch: expected %s, got %s", expectedProjectID, info.Audience)
	}

	return &info, nil
}

// FirebaseAuthRequest defines the payload expected by POST /api/v1/auth/firebase.
type FirebaseAuthRequest struct {
	IDToken string `json:"id_token"`
	Role    string `json:"role"`
}

// HandleFirebaseAuth verifies the Firebase ID token, upserts the user in PostgreSQL, and returns a session JWT.
func HandleFirebaseAuth(pool *postgres.Pool, jwtSecret, projectID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body FirebaseAuthRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			httpx.Abort(c, errs.Invalid("INVALID_BODY", "id_token is required"))
			return
		}

		info, err := VerifyFirebaseIDToken(c.Request.Context(), body.IDToken, projectID)
		if err != nil {
			httpx.Abort(c, errs.Unauthorized("INVALID_TOKEN", "failed to verify Firebase Google token: %v", err))
			return
		}

		// Deterministic UUID based on Firebase sub (Google UID)
		userID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("firebase:"+info.Subject))

		displayName := strings.TrimSpace(info.Name)
		if len(displayName) < 2 {
			if strings.Contains(info.Email, "@") {
				displayName = strings.Split(info.Email, "@")[0]
			} else {
				displayName = "Cinema Patron"
			}
		}
		if len(displayName) > 60 {
			displayName = displayName[:60]
		}

		emailVerified := strings.EqualFold(info.EmailVerified, "true")

		const query = `
			INSERT INTO users (id, email, password_hash, display_name, role, email_verified, avatar_key, status)
			VALUES ($1, $2, 'firebase_oauth', $3, 'USER', $4, $5, 'ACTIVE')
			ON CONFLICT (email) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				avatar_key = EXCLUDED.avatar_key,
				email_verified = EXCLUDED.email_verified,
				updated_at = now()
			RETURNING id, email, display_name, role, avatar_key;
		`

		var (
			dbID     uuid.UUID
			dbEmail  string
			dbName   string
			dbRole   string
			dbAvatar *string
		)

		err = pool.QueryRow(c.Request.Context(), query, userID, info.Email, displayName, emailVerified, info.Picture).
			Scan(&dbID, &dbEmail, &dbName, &dbRole, &dbAvatar)
		if err != nil {
			_ = c.Error(fmt.Errorf("upsert user in postgres: %w", err))
			return
		}

		appRole := "CREATOR"
		if strings.EqualFold(body.Role, "BACKER") {
			appRole = "BACKER"
		}

		token, err := GenerateJWT(dbID, appRole, jwtSecret, 7*24*time.Hour)
		if err != nil {
			_ = c.Error(fmt.Errorf("generate jwt: %w", err))
			return
		}

		avatarOut := info.Picture
		if dbAvatar != nil && *dbAvatar != "" {
			avatarOut = *dbAvatar
		}

		c.JSON(http.StatusOK, gin.H{
			"token": token,
			"user": gin.H{
				"id":     dbID.String(),
				"email":  dbEmail,
				"name":   dbName,
				"role":   appRole,
				"avatar": avatarOut,
			},
		})
	}
}

