package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amwangfan/omnireader/server/internal/auth"
	"github.com/amwangfan/omnireader/server/internal/books"
	"github.com/amwangfan/omnireader/server/internal/db"
	"github.com/amwangfan/omnireader/server/internal/storage"
	_ "modernc.org/sqlite"
)

func TestHealthz(t *testing.T) {
	handler := NewHandler(Options{BuildInfo: BuildInfo{Version: "test"}})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["service"] != "omnireader" || body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestLoginAndMe(t *testing.T) {
	handler := testAuthHandler(t)

	loginBody := bytes.NewBufferString(`{"username":"admin","password":"password","clientLabel":"test"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)

	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginRes.Code, loginRes.Body.String())
	}
	var loginPayload map[string]string
	if err := json.NewDecoder(loginRes.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginPayload["accessToken"] == "" || loginPayload["refreshToken"] == "" {
		t.Fatalf("missing tokens: %#v", loginPayload)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginPayload["accessToken"])
	meRes := httptest.NewRecorder()
	handler.ServeHTTP(meRes, meReq)

	if meRes.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meRes.Code, meRes.Body.String())
	}
	var mePayload map[string]string
	if err := json.NewDecoder(meRes.Body).Decode(&mePayload); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if mePayload["username"] != "admin" {
		t.Fatalf("unexpected me payload: %#v", mePayload)
	}
}

func TestRefreshAndLogoutEndpoints(t *testing.T) {
	handler := testAuthHandler(t)
	loginBody := bytes.NewBufferString(`{"username":"admin","password":"password","clientLabel":"test"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginRes.Code, loginRes.Body.String())
	}
	var loginPayload map[string]string
	if err := json.NewDecoder(loginRes.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	refreshBody := bytes.NewBufferString(`{"refreshToken":"` + loginPayload["refreshToken"] + `"}`)
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", refreshBody)
	refreshRes := httptest.NewRecorder()
	handler.ServeHTTP(refreshRes, refreshReq)
	if refreshRes.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshRes.Code, refreshRes.Body.String())
	}
	var refreshPayload map[string]string
	if err := json.NewDecoder(refreshRes.Body).Decode(&refreshPayload); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if refreshPayload["accessToken"] == "" {
		t.Fatalf("missing refreshed access token: %#v", refreshPayload)
	}

	logoutBody := bytes.NewBufferString(`{"refreshToken":"` + loginPayload["refreshToken"] + `"}`)
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", logoutBody)
	logoutRes := httptest.NewRecorder()
	handler.ServeHTTP(logoutRes, logoutReq)
	if logoutRes.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", logoutRes.Code, logoutRes.Body.String())
	}

	refreshAgainBody := bytes.NewBufferString(`{"refreshToken":"` + loginPayload["refreshToken"] + `"}`)
	refreshAgainReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", refreshAgainBody)
	refreshAgainRes := httptest.NewRecorder()
	handler.ServeHTTP(refreshAgainRes, refreshAgainReq)
	if refreshAgainRes.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d", refreshAgainRes.Code)
	}
}

func TestMeRejectsAnonymousRequest(t *testing.T) {
	handler := testAuthHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestBookUploadListDownloadAndArchive(t *testing.T) {
	handler := testAuthHandler(t)
	token := loginForTest(t, handler)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("title", "Uploaded")
	file, err := writer.CreateFormFile("file", "uploaded.epub")
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	_, _ = file.Write([]byte("epub data"))
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/v1/books", &body)
	uploadReq.Header.Set("Authorization", "Bearer "+token)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRes := httptest.NewRecorder()
	handler.ServeHTTP(uploadRes, uploadReq)
	if uploadRes.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", uploadRes.Code, uploadRes.Body.String())
	}
	var uploadPayload struct {
		Book struct {
			ID string `json:"id"`
		} `json:"book"`
	}
	if err := json.NewDecoder(uploadRes.Body).Decode(&uploadPayload); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadPayload.Book.ID == "" {
		t.Fatal("uploaded book id is required")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/books", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRes.Code, listRes.Body.String())
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/books/"+uploadPayload.Book.ID+"/download", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+token)
	downloadRes := httptest.NewRecorder()
	handler.ServeHTTP(downloadRes, downloadReq)
	if downloadRes.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", downloadRes.Code, downloadRes.Body.String())
	}
	if downloadRes.Body.String() != "epub data" {
		t.Fatalf("download body = %q", downloadRes.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/books/"+uploadPayload.Book.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteRes := httptest.NewRecorder()
	handler.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRes.Code, deleteRes.Body.String())
	}
}

func TestBookEndpointsRejectAnonymousRequests(t *testing.T) {
	handler := testAuthHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/books", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func testAuthHandler(t *testing.T) http.Handler {
	t.Helper()
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.RunMigrations(ctx, conn); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	service, err := auth.NewService(conn, auth.Options{
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
	if err := service.BootstrapAdmin(ctx); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal returned error: %v", err)
	}
	bookService, err := books.NewService(conn, store, books.Options{
		Now: func() time.Time {
			return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("books.NewService returned error: %v", err)
	}
	return NewHandler(Options{
		BuildInfo:   BuildInfo{Version: "test"},
		AuthService: service,
		BookService: bookService,
	})
}

func loginForTest(t *testing.T, handler http.Handler) string {
	t.Helper()
	loginBody := bytes.NewBufferString(`{"username":"admin","password":"password","clientLabel":"test"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginRes.Code, loginRes.Body.String())
	}
	var loginPayload map[string]string
	if err := json.NewDecoder(loginRes.Body).Decode(&loginPayload); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return loginPayload["accessToken"]
}
