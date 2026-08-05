package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func assertStatus(t *testing.T, handler http.Handler, path string, expected int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != expected {
		t.Fatalf("GET %s status = %d, want %d", path, response.Code, expected)
	}
}
