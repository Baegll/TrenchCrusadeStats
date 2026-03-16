package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyAuth_Valid(t *testing.T) {
	handler := APIKeyAuth("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Api-Key", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAPIKeyAuth_Invalid(t *testing.T) {
	handler := APIKeyAuth("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Api-Key", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAPIKeyAuth_Missing(t *testing.T) {
	handler := APIKeyAuth("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestBasicAuth_Valid(t *testing.T) {
	handler := BasicAuth("admin", "pass")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("admin", "pass")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestBasicAuth_Invalid(t *testing.T) {
	handler := BasicAuth("admin", "pass")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate header")
	}
}

func TestBasicAuth_NoAuth(t *testing.T) {
	handler := BasicAuth("admin", "pass")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRateLimit_Middleware(t *testing.T) {
	rl := newRateLimiter(2)
	defer rl.Close()

	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Api-Key", "k")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("request %d: status = %d, want 200", i+1, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Api-Key", "k")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Errorf("3rd request: status = %d, want 429", w.Code)
	}
}
