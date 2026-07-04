package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 30 * 24 * time.Hour
)

type AccessClaims struct {
	Subject  string    `json:"sub"`
	Username string    `json:"username"`
	Expires  time.Time `json:"exp"`
}

func SignAccessToken(secret string, claims AccessClaims) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("token secret is required")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadText := base64.RawURLEncoding.EncodeToString(payload)
	signature := sign(secret, payloadText)
	return "v1." + payloadText + "." + signature, nil
}

func VerifyAccessToken(secret string, token string, now time.Time) (AccessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return AccessClaims{}, errors.New("invalid access token format")
	}
	wantSignature := sign(secret, parts[1])
	if !hmac.Equal([]byte(wantSignature), []byte(parts[2])) {
		return AccessClaims{}, errors.New("invalid access token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessClaims{}, fmt.Errorf("decode access token payload: %w", err)
	}
	var claims AccessClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AccessClaims{}, fmt.Errorf("decode access token claims: %w", err)
	}
	if !claims.Expires.After(now) {
		return AccessClaims{}, errors.New("access token expired")
	}
	return claims, nil
}

func NewRefreshToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, HashRefreshToken(token), nil
}

func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sign(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
