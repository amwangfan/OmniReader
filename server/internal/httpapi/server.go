package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/amwangfan/omnireader/server/internal/auth"
)

type BuildInfo struct {
	Version string
}

type Options struct {
	BuildInfo   BuildInfo
	AuthService *auth.Service
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
