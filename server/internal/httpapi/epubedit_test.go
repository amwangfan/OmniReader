package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amwangfan/omnireader/server/internal/books"
)

func TestEPUBEditorAPIContentMutationConflictAndValidation(t *testing.T) {
	handler := testAuthHandler(t)
	token := loginForTest(t, handler)
	bookID := uploadEditorBookForTest(t, handler, token)

	unauthorized := performJSON(handler, http.MethodGet, "/api/v1/books/"+bookID+"/content", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized content status = %d", unauthorized.Code)
	}

	content := performJSON(handler, http.MethodGet, "/api/v1/books/"+bookID+"/content", "", token)
	if content.Code != http.StatusOK {
		t.Fatalf("content status = %d body=%s", content.Code, content.Body.String())
	}
	var initial books.ContentView
	if err := json.NewDecoder(content.Body).Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if initial.Content.Metadata.Title != "Editor Fixture" || len(initial.Content.Chapters) != 2 {
		t.Fatalf("content = %#v", initial.Content)
	}
	baseRevision := initial.Book.ContentRevision.Format("2006-01-02T15:04:05.000000000Z")

	metadataBody := `{"baseRevision":"` + baseRevision + `","changeSummary":"rename","metadata":{"title":"Changed","author":"Writer","language":"zh-CN","publisher":"Press","description":"Summary","identifier":"id-2"}}`
	metadata := performJSON(handler, http.MethodPut, "/api/v1/books/"+bookID+"/metadata", metadataBody, token)
	if metadata.Code != http.StatusOK {
		t.Fatalf("metadata status = %d body=%s", metadata.Code, metadata.Body.String())
	}
	var published books.PublishResult
	if err := json.NewDecoder(metadata.Body).Decode(&published); err != nil {
		t.Fatal(err)
	}
	if published.Book.Title != "Changed" || published.Revision.ChangeType != "metadata" {
		t.Fatalf("published = %#v", published)
	}

	stale := performJSON(handler, http.MethodPut, "/api/v1/books/"+bookID+"/metadata", metadataBody, token)
	if stale.Code != http.StatusConflict || !bytes.Contains(stale.Body.Bytes(), []byte(`"currentRevision"`)) {
		t.Fatalf("stale status = %d body=%s", stale.Code, stale.Body.String())
	}

	chapter := performJSON(handler, http.MethodGet, "/api/v1/books/"+bookID+"/chapters/chapter-one", "", token)
	if chapter.Code != http.StatusOK || !bytes.Contains(chapter.Body.Bytes(), []byte("Hello")) {
		t.Fatalf("chapter status = %d body=%s", chapter.Code, chapter.Body.String())
	}
	newRevision := published.Book.ContentRevision.Format("2006-01-02T15:04:05.000000000Z")
	invalid := performJSON(handler, http.MethodPut, "/api/v1/books/"+bookID+"/chapters/chapter-one", `{"baseRevision":"`+newRevision+`","title":"Broken","source":"<html><body><script>x</script></body></html>"}`, token)
	if invalid.Code != http.StatusBadRequest || !bytes.Contains(invalid.Body.Bytes(), []byte(`"invalid_content"`)) {
		t.Fatalf("invalid chapter status = %d body=%s", invalid.Code, invalid.Body.String())
	}

	revisions := performJSON(handler, http.MethodGet, "/api/v1/books/"+bookID+"/revisions", "", token)
	if revisions.Code != http.StatusOK || !bytes.Contains(revisions.Body.Bytes(), []byte(`"original":true`)) {
		t.Fatalf("revisions status = %d body=%s", revisions.Code, revisions.Body.String())
	}
}

func TestEPUBEditorAPIRejectsUnknownAndTrailingJSON(t *testing.T) {
	handler := testAuthHandler(t)
	token := loginForTest(t, handler)
	bookID := uploadEditorBookForTest(t, handler, token)
	content := performJSON(handler, http.MethodGet, "/api/v1/books/"+bookID+"/content", "", token)
	var initial books.ContentView
	_ = json.NewDecoder(content.Body).Decode(&initial)
	base := initial.Book.ContentRevision.Format("2006-01-02T15:04:05.000000000Z")

	for _, body := range []string{
		`{"baseRevision":"` + base + `","metadata":{"title":"X"},"surprise":true}`,
		`{"baseRevision":"` + base + `","metadata":{"title":"X"}} {}`,
	} {
		response := performJSON(handler, http.MethodPut, "/api/v1/books/"+bookID+"/metadata", body, token)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func uploadEditorBookForTest(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "editor.epub")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(editorFixtureEPUB(t))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/books", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Book books.Book `json:"book"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Book.ID
}

func editorFixtureEPUB(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	addFixtureZipFile(t, writer, "mimetype", "application/epub+zip")
	addFixtureZipFile(t, writer, "META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`)
	addFixtureZipFile(t, writer, "OPS/content.opf", `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Editor Fixture</dc:title><dc:creator>Author</dc:creator></metadata><manifest><item id="chapter-one" href="one.xhtml" media-type="application/xhtml+xml"/><item id="chapter-two" href="two.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter-one"/><itemref idref="chapter-two"/></spine></package>`)
	addFixtureZipFile(t, writer, "OPS/one.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>One</title></head><body><p>Hello</p></body></html>`)
	addFixtureZipFile(t, writer, "OPS/two.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Two</title></head><body><p>World</p></body></html>`)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
