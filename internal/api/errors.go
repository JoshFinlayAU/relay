package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// apiError is the JSON error envelope per CLAUDE.md:
// {"error": {"code": "...", "message": "..."}}.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}

// Common error responses.
func errBadRequest(w http.ResponseWriter, code, msg string) {
	writeError(w, http.StatusBadRequest, code, msg)
}
func errNotFound(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusNotFound, "not_found", msg)
}
func errConflict(w http.ResponseWriter, code, msg string) {
	writeError(w, http.StatusConflict, code, msg)
}
func errInternal(w http.ResponseWriter, log *slog.Logger, where string, err error) {
	// A cancelled/expired request context means the client went away - not a
	// server fault; don't log it at error level or dress it as a 500.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeError(w, 499, "client_closed", "request cancelled")
		return
	}
	if log != nil {
		log.Error("internal error", "where", where, "err", err)
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)) // 1 MiB cap
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
