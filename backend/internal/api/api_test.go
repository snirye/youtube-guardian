package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type healthStub struct {
	err error
}

func (stub healthStub) PingContext(context.Context) error {
	return stub.err
}

func TestHealth(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		authorization string
		databaseError error
		status        int
		body          string
	}{
		{"healthy", http.MethodGet, "Bearer test-token", nil, http.StatusOK, "{\"status\":\"ok\"}\n"},
		{"unavailable", http.MethodGet, "Bearer test-token", errors.New("internal failure"), http.StatusServiceUnavailable, "{\"status\":\"unavailable\"}\n"},
		{"no token", http.MethodGet, "", nil, http.StatusUnauthorized, "unauthorized\n"},
		{"wrong token", http.MethodGet, "Bearer other-token", nil, http.StatusUnauthorized, "unauthorized\n"},
		{"wrong scheme", http.MethodGet, "Basic test-token", nil, http.StatusUnauthorized, "unauthorized\n"},
		{"wrong method", http.MethodPost, "Bearer test-token", nil, http.StatusMethodNotAllowed, "Method Not Allowed\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/health", nil)
			req.Header.Set("Authorization", tc.authorization)
			res := httptest.NewRecorder()
			New(healthStub{err: tc.databaseError}, "test-token", nil).ServeHTTP(res, req)
			if res.Code != tc.status || res.Body.String() != tc.body {
				t.Fatalf("response = %d %q, want %d %q", res.Code, res.Body.String(), tc.status, tc.body)
			}
		})
	}
}

func TestExtensionOrigin(t *testing.T) {
	handler := New(healthStub{}, "test-token", nil)
	for _, tc := range []struct {
		method, origin string
		status         int
	}{
		{http.MethodGet, extensionOrigin, http.StatusOK},
		{http.MethodOptions, extensionOrigin, http.StatusNoContent},
		{http.MethodGet, "https://www.youtube.com", http.StatusForbidden},
		{http.MethodOptions, "chrome-extension://other", http.StatusForbidden},
	} {
		req := httptest.NewRequest(tc.method, "/health", nil)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Authorization", "Bearer test-token")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.origin, res.Code, tc.status)
		}
		if allowed := res.Header().Get("Access-Control-Allow-Origin"); (tc.origin == extensionOrigin && allowed != extensionOrigin) ||
			(tc.origin != extensionOrigin && allowed != "") {
			t.Fatalf("%s received unexpected CORS origin %q", tc.origin, allowed)
		}
	}
}
