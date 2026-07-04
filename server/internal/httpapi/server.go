package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
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
		mux.HandleFunc("POST /login", webLogin(opts.AuthService))
	}
	if opts.AuthService != nil && opts.BookService != nil {
		mux.HandleFunc("GET /api/v1/books", listBooks(opts.AuthService, opts.BookService))
		mux.HandleFunc("POST /api/v1/books", uploadBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("GET /api/v1/books/{bookID}/download", downloadBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("DELETE /api/v1/books/{bookID}", archiveBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("GET /admin/books", booksPage(opts.AuthService, opts.BookService))
		mux.HandleFunc("POST /admin/books/upload", webUploadBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("POST /admin/books/{bookID}/delete", webDeleteBook(opts.AuthService, opts.BookService))
		mux.HandleFunc("GET /admin/settings", settingsPage(opts.AuthService, opts.BookService))
		mux.HandleFunc("POST /admin/settings/filename-template", updateFilenameTemplate(opts.AuthService, opts.BookService))
		mux.HandleFunc("POST /admin/settings/password", updatePassword(opts.AuthService))
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
    <form method="post" action="/login">
      <p><input type="text" name="username" placeholder="Username" autocomplete="username" required></p>
      <p><input type="password" name="password" placeholder="Password" autocomplete="current-password" required></p>
      <p><button type="submit">Login</button></p>
    </form>
  </main>
</body>
</html>`))
}

func webLogin(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		result, err := service.Login(r.Context(), r.FormValue("username"), r.FormValue("password"), "web-admin")
		if err != nil {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "omnireader_access",
			Value:    result.AccessToken,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  result.ExpiresAt,
		})
		http.Redirect(w, r, "/admin/books", http.StatusSeeOther)
	}
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
		book, err := createBookFromMultipart(r, bookService)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(path.Base(book.StorageKey))+`"`)
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
		if err := bookService.Delete(r.Context(), r.PathValue("bookID")); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete_failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func booksPage(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	page := template.Must(template.New("books").Funcs(template.FuncMap{
		"formatBytes": formatBytes,
	}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OmniReader Books</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f6f1e8;
      --card: rgba(255, 252, 246, 0.88);
      --text: #252018;
      --muted: #776b5d;
      --line: rgba(81, 62, 38, 0.14);
      --accent: #7a4f2a;
      --accent-2: #1f6f5b;
      --danger: #9b2f2f;
      --shadow: 0 18px 60px rgba(52, 38, 21, 0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: ui-serif, "Iowan Old Style", "Noto Serif SC", Georgia, serif;
      color: var(--text);
      background:
        radial-gradient(circle at 20% 0%, rgba(250, 220, 160, .45), transparent 32rem),
        linear-gradient(135deg, #fbf7ef, var(--bg));
    }
    header {
      max-width: 1120px;
      margin: 0 auto;
      padding: 42px 24px 20px;
      display: flex;
      justify-content: space-between;
      gap: 24px;
      align-items: end;
    }
    .eyebrow {
      margin: 0 0 8px;
      color: var(--accent-2);
      font: 700 12px/1.2 ui-sans-serif, system-ui, sans-serif;
      letter-spacing: .16em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0;
      font-size: clamp(36px, 5vw, 64px);
      line-height: .95;
      letter-spacing: -.045em;
    }
    .subtitle {
      margin: 12px 0 0;
      color: var(--muted);
      max-width: 620px;
      font: 15px/1.7 ui-sans-serif, system-ui, sans-serif;
    }
    .stat {
      min-width: 132px;
      border: 1px solid var(--line);
      border-radius: 22px;
      padding: 16px 18px;
      background: rgba(255,255,255,.46);
      text-align: right;
      box-shadow: var(--shadow);
    }
    .stat strong { display: block; font-size: 34px; line-height: 1; }
    .stat span { color: var(--muted); font: 13px ui-sans-serif, system-ui, sans-serif; }
    main {
      max-width: 1120px;
      margin: 0 auto;
      padding: 0 24px 48px;
      display: grid;
      grid-template-columns: minmax(280px, 360px) 1fr;
      gap: 22px;
      align-items: start;
    }
    .panel {
      border: 1px solid var(--line);
      border-radius: 28px;
      background: var(--card);
      box-shadow: var(--shadow);
      backdrop-filter: blur(14px);
      padding: 24px;
    }
    h2 { margin: 0 0 16px; font-size: 22px; letter-spacing: -.02em; }
    label {
      display: block;
      margin: 14px 0 7px;
      color: var(--muted);
      font: 700 12px/1.2 ui-sans-serif, system-ui, sans-serif;
      text-transform: uppercase;
      letter-spacing: .08em;
    }
    input[type="text"], input[type="file"] {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 12px 13px;
      background: rgba(255,255,255,.68);
      color: var(--text);
      font: 15px ui-sans-serif, system-ui, sans-serif;
    }
    button, .button {
      border: 0;
      border-radius: 999px;
      padding: 11px 16px;
      background: var(--accent);
      color: #fff;
      cursor: pointer;
      font: 700 14px ui-sans-serif, system-ui, sans-serif;
      text-decoration: none;
      display: inline-flex;
      align-items: center;
      gap: 8px;
    }
    button:hover, .button:hover { filter: brightness(.96); transform: translateY(-1px); }
    .button.secondary { background: var(--accent-2); }
    .button.danger, button.danger { background: var(--danger); }
    .actions { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 14px; }
    .flash {
      grid-column: 1 / -1;
      border-radius: 18px;
      padding: 13px 16px;
      font: 14px ui-sans-serif, system-ui, sans-serif;
      border: 1px solid var(--line);
      background: rgba(31,111,91,.12);
      color: var(--accent-2);
    }
    .flash.error { background: rgba(155,47,47,.10); color: var(--danger); }
    .library {
      display: grid;
      gap: 14px;
    }
    .book {
      border: 1px solid var(--line);
      border-radius: 22px;
      padding: 18px;
      background: rgba(255,255,255,.52);
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 16px;
      align-items: center;
    }
    .book-title { margin: 0; font-size: 20px; letter-spacing: -.02em; }
    .meta {
      margin: 8px 0 0;
      color: var(--muted);
      font: 13px/1.5 ui-sans-serif, system-ui, sans-serif;
    }
    .empty {
      border: 1px dashed var(--line);
      border-radius: 22px;
      padding: 32px;
      color: var(--muted);
      text-align: center;
      font: 15px/1.7 ui-sans-serif, system-ui, sans-serif;
    }
    @media (max-width: 820px) {
      header { align-items: start; flex-direction: column; }
      .stat { text-align: left; }
      main { grid-template-columns: 1fr; }
      .book { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header>
    <section>
      <p class="eyebrow">Personal library sync</p>
      <h1>OmniReader</h1>
      <p class="subtitle">Upload EPUBs here, then let Android clients pull the library and reading progress from this server.</p>
      <p class="subtitle"><a href="/admin/settings">Settings</a></p>
    </section>
    <aside class="stat">
      <strong>{{len .Books}}</strong>
      <span>EPUB books</span>
    </aside>
  </header>
  <main>
    {{if .Flash}}<div class="flash {{.FlashKind}}">{{.Flash}}</div>{{end}}
    <section class="panel">
      <h2>Add a book</h2>
      <form method="post" action="/admin/books/upload" enctype="multipart/form-data">
        <label for="file">EPUB file</label>
        <input id="file" type="file" name="file" accept=".epub,application/epub+zip" required>
        <label for="title">Title override</label>
        <input id="title" type="text" name="title" placeholder="Leave blank to use filename">
        <label for="author">Author</label>
        <input id="author" type="text" name="author" placeholder="Optional">
        <div class="actions">
          <button type="submit">Upload EPUB</button>
        </div>
      </form>
    </section>
    <section class="panel">
      <h2>Library</h2>
      <div class="library">
      {{range .Books}}
      <article class="book">
        <div>
          <h3 class="book-title">{{.Title}}</h3>
          <p class="meta">{{if .Author}}{{.Author}} · {{end}}{{formatBytes .FileSize}} · {{.Format}}</p>
        </div>
        <div class="actions">
          <a class="button secondary" href="/api/v1/books/{{.ID}}/download">Download</a>
          <form method="post" action="/admin/books/{{.ID}}/delete" onsubmit="return confirm('Delete this book from the server? This removes the saved EPUB file.');">
            <button class="danger" type="submit">Delete</button>
          </form>
        </div>
      </article>
      {{else}}
      <div class="empty">No books uploaded yet. Pick an EPUB on the left to start the library.</div>
      {{end}}
      </div>
    </section>
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
		_ = page.Execute(w, map[string]any{
			"Books":     result,
			"Flash":     flashMessage(r.URL.Query().Get("status"), r.URL.Query().Get("error")),
			"FlashKind": flashKind(r.URL.Query().Get("error")),
		})
	}
}

func webUploadBook(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		if _, err := createBookFromMultipart(r, bookService); err != nil {
			http.Redirect(w, r, "/admin/books?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/books?status=uploaded", http.StatusSeeOther)
	}
}

func webDeleteBook(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		if err := bookService.Delete(r.Context(), r.PathValue("bookID")); err != nil {
			http.Redirect(w, r, "/admin/books?error="+url.QueryEscape("delete failed"), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/books?status=deleted", http.StatusSeeOther)
	}
}

func settingsPage(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	page := template.Must(template.New("settings").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>OmniReader Settings</title>
  <style>
    body { margin: 0; min-height: 100vh; font-family: ui-sans-serif, system-ui, sans-serif; color: #252018; background: linear-gradient(135deg,#fbf7ef,#f1e5d2); }
    main { max-width: 880px; margin: 0 auto; padding: 44px 24px; }
    a { color: #1f6f5b; }
    h1 { font-family: ui-serif, Georgia, serif; font-size: clamp(34px, 5vw, 56px); margin: 0 0 10px; letter-spacing: -.04em; }
    .subtitle { color: #776b5d; margin: 0 0 26px; line-height: 1.7; }
    .panel { border: 1px solid rgba(81,62,38,.14); border-radius: 28px; background: rgba(255,252,246,.9); box-shadow: 0 18px 60px rgba(52,38,21,.12); padding: 24px; margin: 18px 0; }
    label { display: block; margin: 14px 0 7px; color: #776b5d; font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .08em; }
    input { width: 100%; border: 1px solid rgba(81,62,38,.14); border-radius: 16px; padding: 12px 13px; background: rgba(255,255,255,.75); font: 15px ui-sans-serif, system-ui, sans-serif; }
    button { border: 0; border-radius: 999px; padding: 11px 16px; background: #7a4f2a; color: #fff; cursor: pointer; font-weight: 800; margin-top: 14px; }
    code { background: rgba(31,111,91,.10); padding: 2px 5px; border-radius: 6px; }
    .flash { border-radius: 18px; padding: 13px 16px; margin: 0 0 18px; background: rgba(31,111,91,.12); color: #1f6f5b; }
    .flash.error { background: rgba(155,47,47,.10); color: #9b2f2f; }
  </style>
</head>
<body>
  <main>
    <p><a href="/admin/books">← Back to library</a></p>
    <h1>Settings</h1>
    <p class="subtitle">Tune how OmniReader stores uploaded EPUB files and rotate the single-user admin password.</p>
    {{if .Flash}}<div class="flash {{.FlashKind}}">{{.Flash}}</div>{{end}}
    <section class="panel">
      <h2>Saved filename pattern</h2>
      <p class="subtitle">Available tokens: <code>{{"{{Book}}"}}</code>, <code>{{"{{Author}}"}}</code>, <code>{{"{{YYMMDD}}"}}</code>, <code>{{"{{YYYYMMDD}}"}}</code>. The <code>.epub</code> suffix is added automatically when omitted.</p>
      <form method="post" action="/admin/settings/filename-template">
        <label for="filename_template">Pattern</label>
        <input id="filename_template" name="filename_template" value="{{.FilenameTemplate}}" placeholder="{{"{{Book}}-{{Author}}.epub"}}">
        <button type="submit">Save filename pattern</button>
      </form>
    </section>
    <section class="panel">
      <h2>Change password</h2>
      <form method="post" action="/admin/settings/password">
        <label for="current_password">Current password</label>
        <input id="current_password" name="current_password" type="password" autocomplete="current-password" required>
        <label for="new_password">New password</label>
        <input id="new_password" name="new_password" type="password" autocomplete="new-password" minlength="8" required>
        <button type="submit">Change password</button>
      </form>
    </section>
  </main>
</body>
</html>`))

	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		pattern, err := bookService.FilenameTemplate(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "settings_failed"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = page.Execute(w, map[string]any{
			"FilenameTemplate": pattern,
			"Flash":            settingsFlashMessage(r.URL.Query().Get("status"), r.URL.Query().Get("error")),
			"FlashKind":        flashKind(r.URL.Query().Get("error")),
		})
	}
}

func updateFilenameTemplate(authService *auth.Service, bookService *books.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/admin/settings?error="+url.QueryEscape("invalid form"), http.StatusSeeOther)
			return
		}
		if err := bookService.SetFilenameTemplate(r.Context(), r.FormValue("filename_template")); err != nil {
			http.Redirect(w, r, "/admin/settings?error="+url.QueryEscape("save failed"), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/admin/settings?status=filename_template_saved", http.StatusSeeOther)
	}
}

func updatePassword(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := requireUser(w, r, authService)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/admin/settings?error="+url.QueryEscape("invalid form"), http.StatusSeeOther)
			return
		}
		if err := authService.ChangePassword(r.Context(), user.ID, r.FormValue("current_password"), r.FormValue("new_password")); err != nil {
			http.Redirect(w, r, "/admin/settings?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "omnireader_access", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func createBookFromMultipart(r *http.Request, bookService *books.Service) (books.Book, error) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return books.Book{}, errors.New("invalid upload form")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return books.Book{}, errors.New("choose an EPUB file first")
	}
	defer file.Close()

	book, err := bookService.Create(r.Context(), books.CreateInput{
		Filename: header.Filename,
		Title:    r.FormValue("title"),
		Author:   r.FormValue("author"),
		Body:     file,
	})
	if err != nil {
		return books.Book{}, err
	}
	return book, nil
}

func requireUser(w http.ResponseWriter, r *http.Request, service *auth.Service) (auth.User, bool) {
	user, err := service.VerifyBearer(r.Context(), authorizationValue(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return auth.User{}, false
	}
	return user, true
}

func authorizationValue(r *http.Request) string {
	if value := r.Header.Get("Authorization"); value != "" {
		return value
	}
	cookie, err := r.Cookie("omnireader_access")
	if err != nil || cookie.Value == "" {
		return ""
	}
	return "Bearer " + cookie.Value
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "book"
	}
	replacer := strings.NewReplacer(`"`, "", "\\", "", "/", "", ":", "", "*", "", "?", "", "<", "", ">", "", "|", "")
	return replacer.Replace(value)
}

func flashMessage(status string, err string) string {
	if err != "" {
		return err
	}
	switch status {
	case "uploaded":
		return "Upload complete. The EPUB is now in your library."
	case "archived":
		return "Book archived. Existing client copies are not deleted automatically."
	case "deleted":
		return "Book deleted from the server."
	default:
		return ""
	}
}

func settingsFlashMessage(status string, err string) string {
	if err != "" {
		return err
	}
	switch status {
	case "filename_template_saved":
		return "Filename pattern saved. New uploads will use it."
	default:
		return ""
	}
}

func flashKind(err string) string {
	if err != "" {
		return "error"
	}
	return ""
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return "1 KB"
	}
	kb := (size + unit - 1) / unit
	if kb < unit {
		return strconv.FormatInt(kb, 10) + " KB"
	}
	mb := float64(size) / (unit * unit)
	return fmt.Sprintf("%.1f MB", mb)
}
