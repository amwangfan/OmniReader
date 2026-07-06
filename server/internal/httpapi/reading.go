package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/amwangfan/omnireader/server/internal/auth"
	"github.com/amwangfan/omnireader/server/internal/reading"
)

const maxReadingJSONBody = 64 << 10

func registerReadingRoutes(mux *http.ServeMux, authService *auth.Service, service *reading.Service) {
	mux.HandleFunc("PUT /api/v1/devices/current", putCurrentDevice(authService, service))
	mux.HandleFunc("GET /api/v1/devices", listReadingDevices(authService, service))
	mux.HandleFunc("GET /api/v1/devices/{deviceID}", getReadingDevice(authService, service))
	mux.HandleFunc("PATCH /api/v1/devices/{deviceID}", patchReadingDevice(authService, service))
	mux.HandleFunc("DELETE /api/v1/devices/{deviceID}", deleteReadingDevice(authService, service))
	mux.HandleFunc("GET /api/v1/books/{bookID}/progress", getBookProgress(authService, service))
	mux.HandleFunc("PUT /api/v1/books/{bookID}/progress", putBookProgress(authService, service))
	mux.HandleFunc("GET /api/v1/devices/{deviceID}/activity", getDeviceActivity(authService, service))
}

func putCurrentDevice(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		var body reading.DeviceInput
		if err := decodeReadingJSON(w, r, &body); err != nil {
			writeReadingError(w, err)
			return
		}
		device, err := service.UpsertDevice(r.Context(), body)
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, device)
	}
}

func listReadingDevices(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		devices, err := service.ListDevices(r.Context())
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
	}
}

func getReadingDevice(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		device, err := service.GetDevice(r.Context(), r.PathValue("deviceID"))
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, device)
	}
}

func patchReadingDevice(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	type request struct {
		DisplayName string `json:"displayName"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		var body request
		if err := decodeReadingJSON(w, r, &body); err != nil {
			writeReadingError(w, err)
			return
		}
		device, err := service.RenameDevice(r.Context(), r.PathValue("deviceID"), body.DisplayName)
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, device)
	}
}

func deleteReadingDevice(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		if err := service.DisableDevice(r.Context(), r.PathValue("deviceID")); err != nil {
			writeReadingError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func getBookProgress(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		result, err := service.GetProgress(r.Context(), r.PathValue("bookID"), r.URL.Query().Get("deviceId"))
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func putBookProgress(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		var body reading.ProgressInput
		if err := decodeReadingJSON(w, r, &body); err != nil {
			writeReadingError(w, err)
			return
		}
		body.BookID = r.PathValue("bookID")
		result, err := service.PutProgress(r.Context(), body)
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func getDeviceActivity(authService *auth.Service, service *reading.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireUser(w, r, authService); !ok {
			return
		}
		result, err := service.DeviceActivity(r.Context(), r.PathValue("deviceID"), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
		if err != nil {
			writeReadingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func decodeReadingJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxReadingJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return reading.ErrValidation
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return reading.ErrValidation
	}
	return nil
}

func writeReadingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, reading.ErrValidation):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
	case errors.Is(err, reading.ErrDeviceDisabled):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "device_disabled"})
	case errors.Is(err, reading.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}
