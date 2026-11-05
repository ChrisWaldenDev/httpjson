package httpjson

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type message struct {
	Message string `json:"message"`
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int // 0 means Decode should succeed
		wantMsg    string
	}{
		{"valid", `{"message":"hi"}`, 0, ""},
		{"empty body", "", 400, "request body is empty"},
		{"invalid JSON", `{"message":`, 400, "invalid JSON body"},
		{"wrong type", `{"message":1}`, 400, `field "message" must be a string`},
		{"unknown field", `{"msg":"hi"}`, 400, `unknown field "msg"`},
		{"trailing data", `{"message":"hi"} {}`, 400, "request body must contain a single JSON value"},
		{"too large", `{"message":"` + strings.Repeat("a", MaxBodyBytes) + `"}`, 413, "request body too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			var got message
			err := Decode(httptest.NewRecorder(), r, &got)

			if tt.wantStatus == 0 {
				if err != nil {
					t.Fatalf("Decode() error = %v, want nil", err)
				}
				if got.Message != "hi" {
					t.Errorf("Message = %q, want %q", got.Message, "hi")
				}
				return
			}
			e, ok := errors.AsType[*Error](err)
			if !ok {
				t.Fatalf("Decode() error = %v, want *Error", err)
			}
			if e.Status != tt.wantStatus || e.Message != tt.wantMsg {
				t.Errorf("Decode() error = %d %q, want %d %q", e.Status, e.Message, tt.wantStatus, tt.wantMsg)
			}
		})
	}
}

func TestDecodeTypeErrors(t *testing.T) {
	type tag struct {
		Label string `json:"label"`
	}
	type request struct {
		ID    int            `json:"id"`
		Small uint8          `json:"small"`
		Price float64        `json:"price"`
		Name  string         `json:"name"`
		OK    bool           `json:"ok"`
		Data  []byte         `json:"data"`
		Tags  []tag          `json:"tags"`
		Meta  map[string]int `json:"meta"`
		Ptr   *int           `json:"ptr"`
	}
	tests := []struct {
		body    string
		wantMsg string
	}{
		{`{"id":"abc"}`, `field "id" must be an integer`},
		{`{"id":1.5}`, `field "id" must be an integer`},
		{`{"id":1e3}`, `field "id" must be an integer`},
		{`{"id":99999999999999999999}`, `field "id" is out of range`},
		{`{"small":300}`, `field "small" is out of range`},
		{`{"small":-1}`, `field "small" is out of range`},
		{`{"price":"x"}`, `field "price" must be a number`},
		{`{"price":1e400}`, `field "price" is out of range`},
		{`{"name":1}`, `field "name" must be a string`},
		{`{"ok":"yes"}`, `field "ok" must be a boolean`},
		{`{"data":1}`, `field "data" must be a base64 string`},
		{`{"tags":{}}`, `field "tags" must be an array`},
		{`{"tags":[{"label":5}]}`, `field "tags.0.label" must be a string`},
		{`{"meta":[]}`, `field "meta" must be an object`},
		{`{"meta":{"a":"b"}}`, `field "meta.a" must be an integer`},
		{`{"ptr":"x"}`, `field "ptr" must be an integer`},
		{`[]`, `request body must be an object`},
		{`"hello"`, `request body must be an object`},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			var got request
			err := Decode(httptest.NewRecorder(), r, &got)

			e, ok := errors.AsType[*Error](err)
			if !ok {
				t.Fatalf("Decode() error = %v, want *Error", err)
			}
			if e.Status != http.StatusBadRequest || e.Message != tt.wantMsg {
				t.Errorf("Decode() error = %d %q, want 400 %q", e.Status, e.Message, tt.wantMsg)
			}
		})
	}
}

func TestWriteErrorLogsWithSlog(t *testing.T) {
	var logs bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

	WriteError(httptest.NewRecorder(), errors.New("db: connection refused"))
	WriteError(httptest.NewRecorder(), Errorf(404, "item 7 not found"))

	got := logs.String()
	if !strings.Contains(got, "level=ERROR") || !strings.Contains(got, `error="db: connection refused"`) {
		t.Errorf("log = %q, want an ERROR with the internal error", got)
	}
	if strings.Contains(got, "item 7 not found") {
		t.Errorf("log = %q, client errors should not be logged", got)
	}
}

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, http.StatusCreated, message{Message: "hi"})

	assertResponse(t, rec, http.StatusCreated, `{"message":"hi"}`)
}

func TestWriteEncodeFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, http.StatusOK, make(chan int)) // channels can't be encoded

	assertResponse(t, rec, http.StatusInternalServerError, `{"error":"internal server error"}`)
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"Errorf", Errorf(404, "item %d not found", 7), 404, `{"error":"item 7 not found"}`},
		{"wrapped", fmt.Errorf("loading: %w", Errorf(409, "conflict")), 409, `{"error":"conflict"}`},
		{"plain error", errors.New("pq: connection refused"), 500, `{"error":"internal server error"}`},
		{"invalid status", Errorf(200, "not an error"), 500, `{"error":"internal server error"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, tt.err)

			assertResponse(t, rec, tt.wantStatus, tt.wantBody)
		})
	}
}

func assertResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantBody string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d", rec.Code, wantStatus)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != wantBody {
		t.Errorf("body = %s, want %s", got, wantBody)
	}
}
