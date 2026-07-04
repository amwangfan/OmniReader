package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestLoginPageRendersStyledForm(t *testing.T) {
	handler := testAuthHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	body := res.Body.String()
	for _, want := range []string{"Self-hosted reading sync", "Welcome back", `name="username"`, `name="password"`, "Enter library"} {
		if !strings.Contains(body, want) {
			t.Fatalf("login page missing %q: %s", want, body)
		}
	}
}

func TestRootRedirectsByAuthState(t *testing.T) {
	handler := testAuthHandler(t)

	anonReq := httptest.NewRequest(http.MethodGet, "/", nil)
	anonRes := httptest.NewRecorder()
	handler.ServeHTTP(anonRes, anonReq)
	if anonRes.Code != http.StatusSeeOther || anonRes.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous root status = %d location = %q", anonRes.Code, anonRes.Header().Get("Location"))
	}

	cookie := webLoginForTest(t, handler)
	authReq := httptest.NewRequest(http.MethodGet, "/", nil)
	authReq.AddCookie(cookie)
	authRes := httptest.NewRecorder()
	handler.ServeHTTP(authRes, authReq)
	if authRes.Code != http.StatusSeeOther || authRes.Header().Get("Location") != "/admin/books" {
		t.Fatalf("authenticated root status = %d location = %q", authRes.Code, authRes.Header().Get("Location"))
	}
}

func TestLoginPageRedirectsAuthenticatedUser(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/books" {
		t.Fatalf("status = %d location = %q", res.Code, res.Header().Get("Location"))
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
	_, _ = file.Write(fixtureEPUBBytes(t, "API Parsed", "API Author"))
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
	if downloadRes.Body.Len() == 0 {
		t.Fatal("download body should not be empty")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/books/"+uploadPayload.Book.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteRes := httptest.NewRecorder()
	handler.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRes.Code, deleteRes.Body.String())
	}
	downloadAgainReq := httptest.NewRequest(http.MethodGet, "/api/v1/books/"+uploadPayload.Book.ID+"/download", nil)
	downloadAgainReq.Header.Set("Authorization", "Bearer "+token)
	downloadAgainRes := httptest.NewRecorder()
	handler.ServeHTTP(downloadAgainRes, downloadAgainReq)
	if downloadAgainRes.Code != http.StatusNotFound {
		t.Fatalf("download after delete status = %d", downloadAgainRes.Code)
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

func TestWebLoginCookieAllowsAdminBooksPage(t *testing.T) {
	handler := testAuthHandler(t)
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password")
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusSeeOther {
		t.Fatalf("web login status = %d, body = %s", loginRes.Code, loginRes.Body.String())
	}
	cookies := loginRes.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected login cookie")
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/admin/books", nil)
	adminReq.AddCookie(cookies[0])
	adminRes := httptest.NewRecorder()
	handler.ServeHTTP(adminRes, adminReq)
	if adminRes.Code != http.StatusOK {
		t.Fatalf("admin books status = %d, body = %s", adminRes.Code, adminRes.Body.String())
	}
	if !strings.Contains(adminRes.Body.String(), "Personal library sync") {
		t.Fatalf("unexpected admin page: %s", adminRes.Body.String())
	}
	if !strings.Contains(adminRes.Body.String(), "__omniAdminNavigation") {
		t.Fatalf("admin page missing navigation script: %s", adminRes.Body.String())
	}
}

func TestWebUploadRedirectsBackToLibrary(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("title", "Browser Upload")
	file, err := writer.CreateFormFile("file", "browser-upload.epub")
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	_, _ = file.Write(fixtureEPUBBytes(t, "Browser Upload", "Browser Author"))
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/admin/books/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.AddCookie(cookie)
	uploadRes := httptest.NewRecorder()
	handler.ServeHTTP(uploadRes, uploadReq)
	if uploadRes.Code != http.StatusSeeOther {
		t.Fatalf("web upload status = %d, body = %s", uploadRes.Code, uploadRes.Body.String())
	}
	if got := uploadRes.Header().Get("Location"); got != "/admin/books?status=uploaded" {
		t.Fatalf("upload redirect = %q", got)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/admin/books?status=uploaded", nil)
	adminReq.AddCookie(cookie)
	adminRes := httptest.NewRecorder()
	handler.ServeHTTP(adminRes, adminReq)
	if adminRes.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body = %s", adminRes.Code, adminRes.Body.String())
	}
	bodyText := adminRes.Body.String()
	if !strings.Contains(bodyText, "Browser Upload") || !strings.Contains(bodyText, "Upload complete") {
		t.Fatalf("admin page missing uploaded book or flash: %s", bodyText)
	}
}

func TestNovelManagementPageUpdatesBookDetails(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)
	token := loginForTest(t, handler)

	bookID := uploadBookForTest(t, handler, token, "Original Title", "Original Author")

	pageReq := httptest.NewRequest(http.MethodGet, "/admin/novels", nil)
	pageReq.AddCookie(cookie)
	pageRes := httptest.NewRecorder()
	handler.ServeHTTP(pageRes, pageReq)
	if pageRes.Code != http.StatusOK {
		t.Fatalf("novels page status = %d, body = %s", pageRes.Code, pageRes.Body.String())
	}
	if !strings.Contains(pageRes.Body.String(), "OmniReader Novel Management") || !strings.Contains(pageRes.Body.String(), "Original Title") {
		t.Fatalf("novels page missing expected content: %s", pageRes.Body.String())
	}

	form := url.Values{}
	form.Set("title", "Updated Title")
	form.Set("author", "Updated Author")
	form.Set("filename", "updated-file")
	updateReq := httptest.NewRequest(http.MethodPost, "/admin/novels/"+bookID, strings.NewReader(form.Encode()))
	updateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateReq.AddCookie(cookie)
	updateRes := httptest.NewRecorder()
	handler.ServeHTTP(updateRes, updateReq)
	if updateRes.Code != http.StatusSeeOther {
		t.Fatalf("update novel status = %d, body = %s", updateRes.Code, updateRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/books", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRes.Code, listRes.Body.String())
	}
	body := listRes.Body.String()
	for _, want := range []string{"Updated Title", "Updated Author", "updated-file.epub"} {
		if !strings.Contains(body, want) {
			t.Fatalf("updated list missing %q: %s", want, body)
		}
	}
}

func TestSyncPageRendersPlaceholder(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/admin/sync", nil)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("sync page status = %d, body = %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "OmniReader Sync") || !strings.Contains(res.Body.String(), "&#24453;&#21516;&#27493;&#20219;&#21153;") {
		t.Fatalf("sync page missing expected content: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "__omniAdminNavigation") {
		t.Fatalf("sync page missing navigation script: %s", res.Body.String())
	}
}

func TestSettingsUpdateFilenameTemplateAndPassword(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)

	form := url.Values{}
	form.Set("filename_template", "{{YYMMDD}}-{{Book}}-{{Author}}-123")
	templateReq := httptest.NewRequest(http.MethodPost, "/admin/settings/filename-template", strings.NewReader(form.Encode()))
	templateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	templateReq.AddCookie(cookie)
	templateRes := httptest.NewRecorder()
	handler.ServeHTTP(templateRes, templateReq)
	if templateRes.Code != http.StatusSeeOther {
		t.Fatalf("template update status = %d, body = %s", templateRes.Code, templateRes.Body.String())
	}

	settingsReq := httptest.NewRequest(http.MethodGet, "/admin/settings?status=filename_template_saved", nil)
	settingsReq.AddCookie(cookie)
	settingsRes := httptest.NewRecorder()
	handler.ServeHTTP(settingsRes, settingsReq)
	if settingsRes.Code != http.StatusOK {
		t.Fatalf("settings status = %d, body = %s", settingsRes.Code, settingsRes.Body.String())
	}
	if !strings.Contains(settingsRes.Body.String(), "{{YYMMDD}}-{{Book}}-{{Author}}-123") {
		t.Fatalf("settings page missing template: %s", settingsRes.Body.String())
	}

	passwordForm := url.Values{}
	passwordForm.Set("current_password", "password")
	passwordForm.Set("new_password", "new-password")
	passwordReq := httptest.NewRequest(http.MethodPost, "/admin/settings/password", strings.NewReader(passwordForm.Encode()))
	passwordReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	passwordReq.AddCookie(cookie)
	passwordRes := httptest.NewRecorder()
	handler.ServeHTTP(passwordRes, passwordReq)
	if passwordRes.Code != http.StatusSeeOther || passwordRes.Header().Get("Location") != "/login" {
		t.Fatalf("password update status = %d location = %q", passwordRes.Code, passwordRes.Header().Get("Location"))
	}

	loginBody := bytes.NewBufferString(`{"username":"admin","password":"new-password","clientLabel":"test"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", loginBody)
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("new password login status = %d, body = %s", loginRes.Code, loginRes.Body.String())
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

func uploadBookForTest(t *testing.T, handler http.Handler, token string, title string, author string) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "fixture.epub")
	if err != nil {
		t.Fatalf("CreateFormFile returned error: %v", err)
	}
	_, _ = file.Write(fixtureEPUBBytes(t, title, author))
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
	return uploadPayload.Book.ID
}

func webLoginForTest(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "password")
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusSeeOther {
		t.Fatalf("web login status = %d, body = %s", loginRes.Code, loginRes.Body.String())
	}
	cookies := loginRes.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected login cookie")
	}
	return cookies[0]
}

func fixtureEPUBBytes(t *testing.T, title string, author string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	addFixtureZipFile(t, writer, "META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`)
	addFixtureZipFile(t, writer, "OPS/content.opf", `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>`+title+`</dc:title>
    <dc:creator>`+author+`</dc:creator>
  </metadata>
</package>`)
	if err := writer.Close(); err != nil {
		t.Fatalf("close fixture epub: %v", err)
	}
	return buffer.Bytes()
}

func addFixtureZipFile(t *testing.T, writer *zip.Writer, name string, body string) {
	t.Helper()
	file, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create fixture file %s: %v", name, err)
	}
	if _, err := file.Write([]byte(body)); err != nil {
		t.Fatalf("write fixture file %s: %v", name, err)
	}
}
