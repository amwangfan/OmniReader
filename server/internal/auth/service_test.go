package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/amwangfan/omnireader/server/internal/db"
	_ "modernc.org/sqlite"
)

func TestBootstrapAdminAndLogin(t *testing.T) {
	ctx := context.Background()
	conn := testDB(t, ctx)
	service := testService(t, conn)

	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin should be idempotent: %v", err)
	}

	result, err := service.Login(ctx, "admin", "password", "test-client")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" || result.UserID == "" {
		t.Fatalf("login result missing tokens or user id: %#v", result)
	}

	user, err := service.VerifyBearer(ctx, "Bearer "+result.AccessToken)
	if err != nil {
		t.Fatalf("VerifyBearer returned error: %v", err)
	}
	if user.Username != "admin" {
		t.Fatalf("username = %q", user.Username)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	ctx := context.Background()
	conn := testDB(t, ctx)
	service := testService(t, conn)
	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}

	if _, err := service.Login(ctx, "admin", "wrong", "test-client"); err == nil {
		t.Fatal("expected wrong password to fail")
	}
}

func TestRefreshAndLogout(t *testing.T) {
	ctx := context.Background()
	conn := testDB(t, ctx)
	service := testService(t, conn)
	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	login, err := service.Login(ctx, "admin", "password", "test-client")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	refreshed, err := service.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.Username != "admin" {
		t.Fatalf("unexpected refresh result: %#v", refreshed)
	}

	if err := service.Logout(ctx, login.RefreshToken); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if _, err := service.Refresh(ctx, login.RefreshToken); err == nil {
		t.Fatal("expected logged-out refresh token to fail")
	}
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	conn := testDB(t, ctx)
	service := testService(t, conn)
	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	login, err := service.Login(ctx, "admin", "password", "test-client")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	if err := service.ChangePassword(ctx, login.UserID, "password", "new-password"); err != nil {
		t.Fatalf("ChangePassword returned error: %v", err)
	}
	if _, err := service.Login(ctx, "admin", "password", "test-client"); err == nil {
		t.Fatal("old password should not work")
	}
	if _, err := service.Login(ctx, "admin", "new-password", "test-client"); err != nil {
		t.Fatalf("new password should work: %v", err)
	}
	if _, err := service.Refresh(ctx, login.RefreshToken); err == nil {
		t.Fatal("old refresh token should be revoked")
	}
}

func TestChangePasswordRejectsWrongCurrentPassword(t *testing.T) {
	ctx := context.Background()
	conn := testDB(t, ctx)
	service := testService(t, conn)
	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	login, err := service.Login(ctx, "admin", "password", "test-client")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	if err := service.ChangePassword(ctx, login.UserID, "wrong", "new-password"); err == nil {
		t.Fatal("wrong current password should fail")
	}
}

func testDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.RunMigrations(ctx, conn); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	return conn
}

func testService(t *testing.T, conn *sql.DB) *Service {
	t.Helper()
	service, err := NewService(conn, Options{
		AdminUsername: "admin",
		AdminPassword: "password",
		TokenSecret:   "test-secret",
		Now: func() time.Time {
			return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	return service
}
