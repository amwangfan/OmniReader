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
	"github.com/amwangfan/omnireader/server/internal/reading"
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
	for _, want := range []string{
		"Self-hosted reading sync",
		"Welcome back",
		`name="username"`,
		`name="password"`,
		"Enter library",
		`<span class="brand-line">Omni</span>`,
		`<span class="brand-line">Reader</span>`,
	} {
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

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/books/"+uploadPayload.Book.ID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+token)
	detailRes := httptest.NewRecorder()
	handler.ServeHTTP(detailRes, detailReq)
	if detailRes.Code != http.StatusOK || !strings.Contains(detailRes.Body.String(), `"sourceFormat":"epub"`) {
		t.Fatalf("detail status = %d, body = %s", detailRes.Code, detailRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/books", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRes.Code, listRes.Body.String())
	}
	searchReq := httptest.NewRequest(http.MethodGet, "/api/v1/books?q=Uploaded", nil)
	searchReq.Header.Set("Authorization", "Bearer "+token)
	searchRes := httptest.NewRecorder()
	handler.ServeHTTP(searchRes, searchReq)
	if searchRes.Code != http.StatusOK || !strings.Contains(searchRes.Body.String(), "Uploaded") {
		t.Fatalf("search status = %d, body = %s", searchRes.Code, searchRes.Body.String())
	}

	conversionReq := httptest.NewRequest(http.MethodGet, "/api/v1/conversion", nil)
	conversionReq.Header.Set("Authorization", "Bearer "+token)
	conversionRes := httptest.NewRecorder()
	handler.ServeHTTP(conversionRes, conversionReq)
	if conversionRes.Code != http.StatusOK || !strings.Contains(conversionRes.Body.String(), "supportedFormats") || !strings.Contains(conversionRes.Body.String(), "mobi") {
		t.Fatalf("conversion status = %d, body = %s", conversionRes.Code, conversionRes.Body.String())
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

func TestNonEPUBUploadReportsUnavailableConverter(t *testing.T) {
	handler := testAuthHandler(t)
	token := loginForTest(t, handler)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("A sample plain text book."))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/books", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "conversion_unavailable") {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
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

func TestAdminPagesUsePersistentShell(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)

	for _, route := range []string{"/admin/books", "/admin/novels", "/admin/sync", "/admin/settings"} {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			req.AddCookie(cookie)
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			body := res.Body.String()
			for _, marker := range []string{
				`id="admin-app"`,
				`class="admin-header"`,
				`class="admin-brand"`,
				`class="admin-nav"`,
				`id="admin-content"`,
				`querySelector("#admin-content")`,
				`replaceWith(nextContent)`,
				`updateActiveNavigation(url.pathname)`,
				`html { overflow-y: scroll; }`,
				`* { box-sizing: border-box; }`,
			} {
				if !strings.Contains(body, marker) {
					t.Fatalf("%s missing %q: %s", route, marker, body)
				}
			}
			if strings.Count(body, `id="admin-app"`) != 1 ||
				strings.Count(body, `class="admin-header"`) != 1 ||
				strings.Count(body, `class="admin-nav"`) != 1 ||
				strings.Count(body, `id="admin-content"`) != 1 {
				t.Fatalf("%s must render one persistent shell: %s", route, body)
			}
			if !strings.Contains(body, `<a class="active" href="`+route+`">`) {
				t.Fatalf("%s missing active navigation item: %s", route, body)
			}
			for _, stale := range []string{
				`querySelector("#admin-app").replaceWith`,
				`querySelector("main").replaceWith`,
			} {
				if strings.Contains(body, stale) {
					t.Fatalf("%s uses stale replacement boundary %q: %s", route, stale, body)
				}
			}
		})
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

func TestSyncPageRendersDeviceDashboardAndActions(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)
	token := loginForTest(t, handler)
	bookID := uploadBookForTest(t, handler, token, "Sync Book", "Author")

	for _, device := range []struct{ id, name, model string }{
		{"11111111-1111-4111-8111-111111111111", "Living Room", "Leaf5"},
		{"22222222-2222-4222-8222-222222222222", "Bedroom", "Tab Mini C"},
	} {
		response := performJSON(handler, http.MethodPut, "/api/v1/devices/current", `{"id":"`+device.id+`","systemName":"`+device.name+`","platform":"android","manufacturer":"Onyx","model":"`+device.model+`","appVersion":"1.2.3"}`, token)
		if response.Code != http.StatusOK {
			t.Fatalf("register %s: %s", device.name, response.Body.String())
		}
	}
	list := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/books", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(list, listReq)
	var booksPayload struct {
		Books []books.Book `json:"books"`
	}
	if err := json.NewDecoder(list.Body).Decode(&booksPayload); err != nil || len(booksPayload.Books) != 1 {
		t.Fatalf("decode books: %#v %v", booksPayload, err)
	}
	revision := booksPayload.Books[0].ContentRevision.Format(time.RFC3339Nano)
	progressBody := `{"deviceId":"11111111-1111-4111-8111-111111111111","locator":{"version":1,"contentRevision":"` + revision + `","chapterHref":"OPS/chapter-2.xhtml","chapterIndex":1,"blockIndex":7,"charOffset":3,"textQuote":"context","textHash":"hash","chapterProgress":0.4,"bookProgress":0.3},"percentage":0.3,"dailyReadSeconds":{"2026-07-04":125}}`
	progress := performJSON(handler, http.MethodPut, "/api/v1/books/"+bookID+"/progress", progressBody, token)
	if progress.Code != http.StatusOK {
		t.Fatalf("save progress: %s", progress.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/sync", nil)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("sync page status = %d, body = %s", res.Code, res.Body.String())
	}
	page := res.Body.String()
	for _, want := range []string{"OmniReader Sync", "Living Room", "Bedroom", "Leaf5", "Tab Mini C", "Sync Book", "Chapter 2", "Block 8", "125s", "Global source", `name="display_name"`, `/admin/sync/devices/11111111-1111-4111-8111-111111111111/disable`, `<details>`, "OPS/chapter-2.xhtml"} {
		if !strings.Contains(page, want) {
			t.Fatalf("sync page missing %q: %s", want, page)
		}
	}
	if strings.Count(page, `id="admin-app"`) != 1 || !strings.Contains(page, "__omniAdminNavigation") {
		t.Fatalf("sync page lost persistent shell: %s", page)
	}

	renameForm := url.Values{"display_name": {"Desk Reader"}}
	renameReq := httptest.NewRequest(http.MethodPost, "/admin/sync/devices/11111111-1111-4111-8111-111111111111/rename", strings.NewReader(renameForm.Encode()))
	renameReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	renameReq.AddCookie(cookie)
	renameRes := httptest.NewRecorder()
	handler.ServeHTTP(renameRes, renameReq)
	if renameRes.Code != http.StatusSeeOther || renameRes.Header().Get("Location") != "/admin/sync?status=renamed" {
		t.Fatalf("rename response=%d location=%q", renameRes.Code, renameRes.Header().Get("Location"))
	}
	disableReq := httptest.NewRequest(http.MethodPost, "/admin/sync/devices/22222222-2222-4222-8222-222222222222/disable", nil)
	disableReq.AddCookie(cookie)
	disableRes := httptest.NewRecorder()
	handler.ServeHTTP(disableRes, disableReq)
	if disableRes.Code != http.StatusSeeOther || disableRes.Header().Get("Location") != "/admin/sync?status=disabled" {
		t.Fatalf("disable response=%d location=%q", disableRes.Code, disableRes.Header().Get("Location"))
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
			return time.Date(2026, 7, 4, 10, 0, 0, 123456789, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("books.NewService returned error: %v", err)
	}
	readingService, err := reading.NewService(conn, reading.Options{Now: func() time.Time {
		return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("reading.NewService returned error: %v", err)
	}
	return NewHandler(Options{
		BuildInfo:      BuildInfo{Version: "test"},
		AuthService:    service,
		BookService:    bookService,
		ReadingService: readingService,
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
