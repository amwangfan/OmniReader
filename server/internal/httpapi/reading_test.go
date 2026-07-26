package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amwangfan/omnireader/server/internal/auth"
	"github.com/amwangfan/omnireader/server/internal/books"
	"github.com/amwangfan/omnireader/server/internal/db"
	"github.com/amwangfan/omnireader/server/internal/reading"
	"github.com/amwangfan/omnireader/server/internal/storage"
	_ "modernc.org/sqlite"
)

const apiDeviceID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

func TestDeviceAPIContractAndLifecycle(t *testing.T) {
	handler, token := testReadingHandler(t)

	unauthorized := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+apiDeviceID+`"}`, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	unknown := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+apiDeviceID+`","systemName":"Leaf 5","platform":"android","manufacturer":"Onyx","model":"Leaf5","appVersion":"1.0","surprise":true}`, token)
	assertAPIError(t, unknown, http.StatusBadRequest, "invalid_request")

	registered := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+apiDeviceID+`","systemName":"Leaf 5","platform":"android","manufacturer":"Onyx","model":"Leaf5","appVersion":"1.0"}`, token)
	if registered.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", registered.Code, registered.Body.String())
	}
	var device reading.Device
	if err := json.NewDecoder(registered.Body).Decode(&device); err != nil {
		t.Fatalf("decode device: %v", err)
	}
	if device.ID != apiDeviceID || device.DisplayName != "Leaf 5" {
		t.Fatalf("unexpected device: %#v", device)
	}

	renamed := performJSON(handler, http.MethodPatch, "/api/v1/devices/"+apiDeviceID, `{"displayName":"Night Reader"}`, token)
	if renamed.Code != http.StatusOK || !strings.Contains(renamed.Body.String(), `"displayName":"Night Reader"`) {
		t.Fatalf("rename status=%d body=%s", renamed.Code, renamed.Body.String())
	}
	listed := performJSON(handler, http.MethodGet, "/api/v1/devices", "", token)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"devices"`) || !strings.Contains(listed.Body.String(), "Night Reader") {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	detail := performJSON(handler, http.MethodGet, "/api/v1/devices/"+apiDeviceID, "", token)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"todayReadSeconds"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	disabled := performJSON(handler, http.MethodDelete, "/api/v1/devices/"+apiDeviceID, "", token)
	if disabled.Code != http.StatusNoContent {
		t.Fatalf("disable status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	stillReadable := performJSON(handler, http.MethodGet, "/api/v1/devices/"+apiDeviceID, "", token)
	if stillReadable.Code != http.StatusOK || !strings.Contains(stillReadable.Body.String(), `"disabledAt"`) {
		t.Fatalf("disabled detail status=%d body=%s", stillReadable.Code, stillReadable.Body.String())
	}
	disabledWrite := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+apiDeviceID+`","systemName":"Leaf"}`, token)
	assertAPIError(t, disabledWrite, http.StatusForbidden, "device_disabled")
}

func TestProgressAndActivityAPIContract(t *testing.T) {
	handler, token := testReadingHandler(t)
	registered := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+apiDeviceID+`","systemName":"Leaf","platform":"android"}`, token)
	if registered.Code != http.StatusOK {
		t.Fatalf("register: %s", registered.Body.String())
	}
	body := `{
      "deviceId":"` + apiDeviceID + `",
      "locator":{"version":1,"contentRevision":"2026-07-06T00:00:00.123456789Z","chapterHref":"OPS/one.xhtml","chapterIndex":0,"blockIndex":4,"charOffset":2,"textQuote":"hello","textHash":"abc","chapterProgress":0.4,"bookProgress":0.2},
      "percentage":0.2,
      "clientUpdatedAt":"2026-07-06T12:00:00Z",
      "dailyReadSeconds":{"2026-07-06":90}
    }`
	put := performJSON(handler, http.MethodPut, "/api/v1/books/book-1/progress", body, token)
	if put.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", put.Code, put.Body.String())
	}
	var progressPayload map[string]json.RawMessage
	if err := json.NewDecoder(put.Body).Decode(&progressPayload); err != nil {
		t.Fatalf("decode put: %v", err)
	}
	for _, key := range []string{"deviceProgress", "globalProgress", "contentRevision"} {
		if _, ok := progressPayload[key]; !ok {
			t.Fatalf("put response missing %q: %#v", key, progressPayload)
		}
	}
	var progress reading.Progress
	if err := json.Unmarshal(progressPayload["deviceProgress"], &progress); err != nil {
		t.Fatalf("decode device progress: %v", err)
	}
	if progress.DeviceID != apiDeviceID || progress.DeviceName != "Leaf" || progress.Locator.BlockIndex != 4 || progress.RevisionMismatch {
		t.Fatalf("unexpected progress: %#v", progress)
	}

	get := performJSON(handler, http.MethodGet, "/api/v1/books/book-1/progress?deviceId="+apiDeviceID, "", token)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"deviceProgress"`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	activity := performJSON(handler, http.MethodGet, "/api/v1/devices/"+apiDeviceID+"/activity?from=2026-07-06&to=2026-07-06", "", token)
	if activity.Code != http.StatusOK || !strings.Contains(activity.Body.String(), `"totalReadSeconds":90`) {
		t.Fatalf("activity status=%d body=%s", activity.Code, activity.Body.String())
	}

	invalid := performJSON(handler, http.MethodPut, "/api/v1/books/book-1/progress", strings.Replace(body, `"version":1`, `"version":2`, 1), token)
	assertAPIError(t, invalid, http.StatusBadRequest, "invalid_request")
	missing := performJSON(handler, http.MethodGet, "/api/v1/books/missing/progress?deviceId="+apiDeviceID, "", token)
	assertAPIError(t, missing, http.StatusNotFound, "not_found")
}

func TestReadingAPIBoundsJSONBodies(t *testing.T) {
	handler, token := testReadingHandler(t)
	oversized := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+apiDeviceID+`","systemName":"`+strings.Repeat("x", 70<<10)+`"}`, token)
	assertAPIError(t, oversized, http.StatusBadRequest, "invalid_request")
	malformed := performJSON(handler, http.MethodPatch, "/api/v1/devices/"+apiDeviceID, `{"displayName":`, token)
	assertAPIError(t, malformed, http.StatusBadRequest, "invalid_request")
}

func testReadingHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	ctx := context.Background()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.RunMigrations(ctx, conn); err != nil {
		t.Fatal(err)
	}
	revision := "2026-07-06T00:00:00.123456789Z"
	if _, err := conn.Exec(`INSERT INTO books (id,title,format,storage_key,file_size,checksum,content_revision,created_at,updated_at) VALUES ('book-1','API Book','epub','book.epub',1,'sum',?,?,?)`, revision, revision, revision); err != nil {
		t.Fatal(err)
	}
	authService, err := auth.NewService(conn, auth.Options{AdminUsername: "admin", AdminPassword: "password", TokenSecret: "test-secret", Now: func() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := authService.BootstrapAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bookService, err := books.NewService(conn, store, books.Options{})
	if err != nil {
		t.Fatal(err)
	}
	readingService, err := reading.NewService(conn, reading.Options{Now: func() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(Options{BuildInfo: BuildInfo{Version: "test"}, AuthService: authService, BookService: bookService, ReadingService: readingService})
	return handler, loginForTest(t, handler)
}

func performJSON(handler http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload["error"] != code {
		t.Fatalf("error=%q want=%q body=%s", payload["error"], code, response.Body.String())
	}
}
