// Package httpjson has helpers for JSON APIs built on net/http: decoding
// request bodies, writing JSON responses, and turning errors into JSON error
// responses with the right status code.
package httpjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
)

// MaxBodyBytes caps the size of a request body read by Decode.
const MaxBodyBytes = 1 << 20 // 1 MiB

// Error is an error that's safe to show the client, with the status code to
// send it with. Create one with Errorf and pass it to WriteError.
type Error struct {
	Status  int
	Message string
}

// Error returns the message, so *Error satisfies the error interface.
func (e *Error) Error() string { return e.Message }

// Errorf returns an *Error with the given status and a formatted message.
// The message is sent to the client as-is.
func Errorf(status int, format string, args ...any) error {
	return &Error{Status: status, Message: fmt.Sprintf(format, args...)}
}

// errorBody is the body of every error response.
type errorBody struct {
	Error string `json:"error"`
}

// Decode reads the request body into v. It rejects empty bodies, bodies over
// MaxBodyBytes, values of the wrong type, fields v doesn't have, and anything
// after the JSON value. The error it returns is always an *Error, so pass it
// straight to WriteError.
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			return Errorf(http.StatusRequestEntityTooLarge, "request body too large")
		}
		if errors.Is(err, io.EOF) {
			return Errorf(http.StatusBadRequest, "request body is empty")
		}
		if te, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			if msg := typeErrorMessage(te); msg != "" {
				return Errorf(http.StatusBadRequest, "%s", msg)
			}
		}
		// encoding/json has no typed error for unknown fields, only this message
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			return Errorf(http.StatusBadRequest, "unknown field %s", field)
		}
		return Errorf(http.StatusBadRequest, "invalid JSON body")
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Errorf(http.StatusBadRequest, "request body must contain a single JSON value")
	}
	return nil
}

// typeErrorMessage describes a JSON value that doesn't fit its Go type, like
// `field "id" must be an integer`. It returns "" if it can't name the type.
func typeErrorMessage(te *json.UnmarshalTypeError) string {
	subject := "request body"
	if te.Field != "" {
		subject = fmt.Sprintf("field %q", te.Field)
	}

	// te.Value is "number 1.5" when a number was the right kind of JSON
	// value but didn't fit: a fraction for an integer, or out of range.
	if num, ok := strings.CutPrefix(te.Value, "number "); ok {
		if isInteger(te.Type.Kind()) && strings.ContainsAny(num, ".eE") {
			return subject + " must be an integer"
		}
		return subject + " is out of range"
	}

	want := jsonTypeName(te.Type)
	if want == "" {
		return ""
	}
	return subject + " must be " + want
}

// jsonTypeName names the JSON value that decodes into t, like "a string".
func jsonTypeName(t reflect.Type) string {
	switch k := t.Kind(); {
	case k == reflect.String:
		return "a string"
	case k == reflect.Bool:
		return "a boolean"
	case isInteger(k):
		return "an integer"
	case k == reflect.Float32 || k == reflect.Float64:
		return "a number"
	case k == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		return "a base64 string" // encoding/json decodes []byte from base64
	case k == reflect.Slice || k == reflect.Array:
		return "an array"
	case k == reflect.Struct || k == reflect.Map:
		return "an object"
	}
	return ""
}

func isInteger(k reflect.Kind) bool {
	return reflect.Int <= k && k <= reflect.Uint64
}

// Write sends v as JSON with the given status. It encodes into a buffer
// first, so an encoding failure can still become a clean 500.
func Write(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		WriteError(w, fmt.Errorf("httpjson: encoding response: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// WriteError sends err as a JSON error response: {"error": "..."}.
//
// If err is or wraps an *Error with a 4xx or 5xx status, its status and
// message are sent. Any other error is logged with slog.Error and sent as a
// 500 without its details, so internal error text never reaches the client.
// The log goes to slog's default logger, so set it with slog.SetDefault to
// control the format and destination.
func WriteError(w http.ResponseWriter, err error) {
	if e, ok := errors.AsType[*Error](err); ok && e.Status >= 400 && e.Status <= 599 {
		writeErrorBody(w, e.Status, e.Message)
		return
	}
	slog.Error("httpjson: internal error", "error", err)
	writeErrorBody(w, http.StatusInternalServerError, "internal server error")
}

func writeErrorBody(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorBody{Error: msg})
}
