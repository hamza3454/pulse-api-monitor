package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"pulse/internal/api/middleware"
	"pulse/internal/domain"
)

// Every error response has the same shape, so clients need one parser:
//
//	{"error": {"code": "validation_failed", "message": "...", "details": ["..."]}}
//
// "code" is stable and meant for programs; "message" is meant for people.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent, so an encoding failure can only be a
	// dropped connection; there is nothing useful left to do with it.
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string, details ...string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message, Details: details}})
}

func writeNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not_found", "monitor not found")
}

// writeValidation reports a domain validation failure as 422 with one detail
// line per problem.
func writeValidation(w http.ResponseWriter, ve *domain.ValidationError) {
	writeError(w, http.StatusUnprocessableEntity, "validation_failed", "the request is not valid", ve.Problems...)
}

// writeServerError logs the real error with the request ID and sends the
// client a generic message, so internals (SQL, hostnames) never leak.
func (h *handlers) writeServerError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "request failed",
		slog.Any("error", err),
		slog.String("request_id", middleware.RequestIDFromContext(r.Context())),
	)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

const maxBodyBytes = 1 << 20 // 1 MiB is far more than any monitor request needs

// decodeJSON reads a single JSON object from the request body into dst. It
// rejects unknown fields, trailing data and oversized bodies. On failure it
// has already written the error response and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	err := dec.Decode(dst)
	if err == nil {
		// A second value after the object means the body is not one JSON document.
		if _, extra := dec.Token(); !errors.Is(extra, io.EOF) {
			err = errors.New("body must contain a single JSON object")
		}
	}
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large")
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid: "+err.Error())
	return false
}
