package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Options struct {
	AdminUsername string
	AdminPassword string
	TokenSecret   string
	Now           func() time.Time
}

type Service struct {
	db            *sql.DB
	adminUsername string
	adminPassword string
	tokenSecret   string
	now           func() time.Time
}

type LoginResult struct {
	UserID       string
	Username     string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type RefreshResult struct {
	UserID      string
	Username    string
	AccessToken string
	ExpiresAt   time.Time
}

type User struct {
	ID       string
	Username string
}

func NewService(db *sql.DB, opts Options) (*Service, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	adminUsername := strings.TrimSpace(opts.AdminUsername)
	if adminUsername == "" {
		return nil, errors.New("admin username is required")
	}
	if strings.TrimSpace(opts.AdminPassword) == "" {
		return nil, errors.New("admin password is required")
	}
	if strings.TrimSpace(opts.TokenSecret) == "" {
		return nil, errors.New("token secret is required")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		db:            db,
		adminUsername: adminUsername,
		adminPassword: opts.AdminPassword,
		tokenSecret:   opts.TokenSecret,
		now:           now,
	}, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	passwordHash, err := HashPassword(s.adminPassword)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	now := s.now().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO users (id, username, password_hash, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
`, newID("usr"), s.adminUsername, passwordHash, now, now)
	if err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}
	return nil
}

func (s *Service) Login(ctx context.Context, username string, password string, clientLabel string) (LoginResult, error) {
	var userID string
	var passwordHash string
	err := s.db.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE username = ?`, strings.TrimSpace(username)).Scan(&userID, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, errors.New("invalid username or password")
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("find user: %w", err)
	}
	if !VerifyPassword(passwordHash, password) {
		return LoginResult{}, errors.New("invalid username or password")
	}

	now := s.now()
	expiresAt := now.Add(AccessTokenTTL)
	accessToken, err := SignAccessToken(s.tokenSecret, AccessClaims{
		Subject:  userID,
		Username: strings.TrimSpace(username),
		Expires:  expiresAt,
	})
	if err != nil {
		return LoginResult{}, err
	}
	refreshToken, refreshHash, err := NewRefreshToken()
	if err != nil {
		return LoginResult{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions (id, user_id, refresh_token_hash, client_label, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, newID("ses"), userID, refreshHash, strings.TrimSpace(clientLabel), now.Add(RefreshTokenTTL).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}

	return LoginResult{
		UserID:       userID,
		Username:     strings.TrimSpace(username),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

func (s *Service) ChangePassword(ctx context.Context, userID string, currentPassword string, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new password is required")
	}
	if len(newPassword) < 8 {
		return errors.New("new password must be at least 8 characters")
	}

	var passwordHash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("user not found")
	}
	if err != nil {
		return fmt.Errorf("find user password: %w", err)
	}
	if !VerifyPassword(passwordHash, currentPassword) {
		return errors.New("current password is incorrect")
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	now := s.now().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password change: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, newHash, now, userID); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now, userID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password change: %w", err)
	}
	return nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (RefreshResult, error) {
	refreshHash := HashRefreshToken(refreshToken)

	var userID string
	var username string
	var expiresAtText string
	var revokedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT users.id, users.username, sessions.expires_at, sessions.revoked_at
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.refresh_token_hash = ?
`, refreshHash).Scan(&userID, &username, &expiresAtText, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshResult{}, errors.New("invalid refresh token")
	}
	if err != nil {
		return RefreshResult{}, fmt.Errorf("find refresh session: %w", err)
	}
	if revokedAt.Valid {
		return RefreshResult{}, errors.New("refresh token revoked")
	}
	refreshExpiresAt, err := time.Parse(time.RFC3339Nano, expiresAtText)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("parse refresh expiry: %w", err)
	}
	now := s.now()
	if !refreshExpiresAt.After(now) {
		return RefreshResult{}, errors.New("refresh token expired")
	}

	accessExpiresAt := now.Add(AccessTokenTTL)
	accessToken, err := SignAccessToken(s.tokenSecret, AccessClaims{
		Subject:  userID,
		Username: username,
		Expires:  accessExpiresAt,
	})
	if err != nil {
		return RefreshResult{}, err
	}

	return RefreshResult{
		UserID:      userID,
		Username:    username,
		AccessToken: accessToken,
		ExpiresAt:   accessExpiresAt,
	}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	now := s.now().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE sessions
SET revoked_at = ?
WHERE refresh_token_hash = ? AND revoked_at IS NULL
`, now, HashRefreshToken(refreshToken))
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func (s *Service) VerifyBearer(ctx context.Context, bearer string) (User, error) {
	token := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	if token == bearer {
		return User{}, errors.New("bearer token is required")
	}
	claims, err := VerifyAccessToken(s.tokenSecret, token, s.now())
	if err != nil {
		return User{}, err
	}

	var user User
	err = s.db.QueryRowContext(ctx, `SELECT id, username FROM users WHERE id = ?`, claims.Subject).Scan(&user.ID, &user.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, errors.New("user not found")
	}
	if err != nil {
		return User{}, fmt.Errorf("find bearer user: %w", err)
	}
	return user, nil
}

func newID(prefix string) string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw)
}
