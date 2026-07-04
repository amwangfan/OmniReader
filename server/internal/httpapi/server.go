package httpapi

import (
	"encoding/json"
	"net/http"
)

type BuildInfo struct {
	Version string
}

func NewHandler(info BuildInfo) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(info))
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
