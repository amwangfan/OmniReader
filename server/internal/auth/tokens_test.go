package auth

import (
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	token, err := SignAccessToken("secret", AccessClaims{
		Subject:  "user-1",
		Username: "admin",
		Expires:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("SignAccessToken returned error: %v", err)
	}

	claims, err := VerifyAccessToken("secret", token, now)
	if err != nil {
		t.Fatalf("VerifyAccessToken returned error: %v", err)
	}
	if claims.Subject != "user-1" || claims.Username != "admin" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestAccessTokenRejectsWrongSecret(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	token, err := SignAccessToken("secret", AccessClaims{
		Subject: "user-1",
		Expires: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("SignAccessToken returned error: %v", err)
	}

	if _, err := VerifyAccessToken("wrong", token, now); err == nil {
		t.Fatal("expected wrong secret to fail")
	}
}

func TestAccessTokenRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	token, err := SignAccessToken("secret", AccessClaims{
		Subject: "user-1",
		Expires: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("SignAccessToken returned error: %v", err)
	}

	if _, err := VerifyAccessToken("secret", token, now); err == nil {
		t.Fatal("expected expired token to fail")
	}
}

func TestRefreshTokenHash(t *testing.T) {
	token, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken returned error: %v", err)
	}
	if token == "" || hash == "" {
		t.Fatal("token and hash are required")
	}
	if HashRefreshToken(token) != hash {
		t.Fatal("refresh token hash should be reproducible")
	}
	if HashRefreshToken("different") == hash {
		t.Fatal("different token should not share the hash")
	}
}
