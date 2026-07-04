package httpapi

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/amwangfan/omnireader/server/internal/auth"
	"github.com/amwangfan/omnireader/server/internal/books"
)

type BuildInfo struct {
	Version string
}

type Options struct {
	BuildInfo   BuildInfo
	AuthService *auth.Service
	BookService *books.Service
}

func NewHandler(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(opts.BuildInfo))
	if opts.AuthService != nil {
		mux.HandleFunc("POST /api/v1/auth/login", login(opts.AuthService))
		mux.HandleFunc("POST /api/v1/auth/refresh", refresh(opts.AuthService))
		mux.HandleFunc("POST /api/v1/auth/logout", logout(opts.AuthService))
		mux.HandleFunc("GET /api/v1/me", me(opts.AuthService))
		mux.HandleFunc("GET /login", loginPage)
	}
	if opts.AuthService != nil && opts.BookService != nil {
		mux.HandleFunc("GET /api/v1/books", listBooks(opts.AuthService, opts.BookService))
		mux.HandleFunc("POST /api/v1/books", uploadBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("GET /api/v1/books/{bookID}/download", downloadBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("DELETE /api/v1/books/{bookID}", archiveBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("GET /admin/books", booksPage(opts.AuthService, opts.BookService))
	}
	return mux
}

func healthz(info BuildInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "omnireader",
			"status":  "ok",
			"version": info.Version,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func login(service *auth.Service) http.HandlerFunc {
	type request struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		ClientLabel string `json:"clientLabel"`
	}
	type response struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		TokenType    string `json:"tokenType"`
		ExpiresAt    string `json:"expiresAt"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		result, err := service.Login(r.Context(), body.Username, body.Password, body.ClientLabel)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			return
		}
		writeJSON(w, http.StatusOK, response{
			AccessToken:  result.AccessToken,
			RefreshToken: result.RefreshToken,
			TokenType:    "Bearer",
			ExpiresAt:    result.ExpiresAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		})
	}
}

func refresh(service *auth.Service) http.HandlerFunc {
	type request struct {
		RefreshToken string `json:"refreshToken"`
	}
	type response struct {
		AccessToken string `json:"accessToken"`
		TokenType   string `json:"tokenType"`
		ExpiresAt   string `json:"expiresAt"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		result, err := service.Refresh(r.Context(), body.RefreshToken)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_refresh_token"})
			return
		}
		writeJSON(w, http.StatusOK, response{
			AccessToken: result.AccessToken,
			TokenType:   "Bearer",
			ExpiresAt:   result.ExpiresAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		})
	}
}

func logout(service *auth.Service) http.HandlerFunc {
	type request struct {
		RefreshToken string `json:"refreshToken"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if err := service.Logout(r.Context(), body.RefreshToken); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout_failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func me(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := service.VerifyBearer(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"id":       user.ID,
			"username": user.Username,
		})
	}
}

func loginPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OmniReader Login</title>
</head>
<body>
  <main>
    <h1>OmniReader</h1>
    <p>Server-rendered admin UI is starting with login/API foundations.</p>
  </main>
</body>
</html>`))
}

func listBooks(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		result, err := bookService.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_books_failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"books": result})
	}
}

func uploadBook(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_multipart_form"})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_required"})
			return
		}
		defer file.Close()

		book, err := bookService.Create(r.Context(), books.CreateInput{
			Filename: header.Filename,
			Title:    r.FormValue("title"),
			Author:   r.FormValue("author"),
			Body:     file,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload_failed"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"book": book})
	}
}

func downloadBook(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		book, reader, err := bookService.Open(r.Context(), r.PathValue("bookID"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "book_not_found"})
			return
		}
		defer reader.Close()

		w.Header().Set("Content-Type", "application/epub+zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(book.Title)+`.epub"`)
		w.Header().Set("X-OmniReader-Book-ID", book.ID)
		if _, err := io.Copy(w, reader); err != nil && !errors.Is(err, http.ErrAbortHandler) {
			return
		}
	}
}

func archiveBook(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		if err := bookService.Archive(r.Context(), r.PathValue("bookID")); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "archive_failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func booksPage(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	page := template.Must(template.New("books").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OmniReader Books</title>
</head>
<body>
  <main>
    <h1>OmniReader Books</h1>
    <form method="post" action="/api/v1/books" enctype="multipart/form-data">
      <p><input type="file" name="file" accept=".epub,application/epub+zip" required></p>
      <p><input type="text" name="title" placeholder="Title"></p>
      <p><input type="text" name="author" placeholder="Author"></p>
      <p><button type="submit">Upload EPUB</button></p>
    </form>
    <h2>Library</h2>
    <ul>
      {{range .Books}}
      <li>{{.Title}} {{if .Author}}— {{.Author}}{{end}} <small>{{.FileSize}} bytes</small></li>
      {{else}}
      <li>No books uploaded yet.</li>
      {{end}}
    </ul>
  </main>
</body>
</html>`))

	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		result, err := bookService.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_books_failed"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, map[string]any{"Books": result})
	}
}

func requireUser(w http.ResponseWriter, r *http.Request, service *auth.Service) (auth.User, bool) {
	user, err := service.VerifyBearer(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return auth.User{}, false
	}
	return user, true
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "book"
	}
	replacer := strings.NewReplacer(`"`, "", "\\", "", "/", "", ":", "", "*", "", "?", "", "<", "", ">", "", "|", "")
	return replacer.Replace(value)
}
