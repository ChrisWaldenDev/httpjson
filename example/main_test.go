package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	tests := []struct {
		method, path, body string
		wantStatus         int
		wantBody           string
	}{
		{"GET", "/api/health", "", 200, `{"status":"ok"}`},
		{"GET", "/api/items/1", "", 200, `{"id":1,"name":"Hammer"}`},
		{"GET", "/api/items/abc", "", 400, `{"error":"item id must be a number"}`},
		{"GET", "/api/items/99", "", 404, `{"error":"item 99 not found"}`},
		{"POST", "/api/items", `{"name":"Wrench"}`, 201, `{"id":2,"name":"Wrench"}`},
		{"GET", "/api/items/2", "", 200, `{"id":2,"name":"Wrench"}`},
		{"POST", "/api/items", `{"name":""}`, 400, `{"error":"name is required"}`},
		{"POST", "/api/items", `{"name":5}`, 400, `{"error":"field \"name\" must be a string"}`},
		{"POST", "/api/items", `{"title":"Wrench"}`, 400, `{"error":"unknown field \"title\""}`},
		{"POST", "/api/items", "", 400, `{"error":"request body is empty"}`},
		{"GET", "/api/error", "", 500, `{"error":"internal server error"}`},
	}
	h := Routes() // the cases share one server, so they run in order
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path+" "+tt.body, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
		})
	}
}
