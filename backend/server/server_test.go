package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/learn/kuber-crd/backend/db"
)

func setupTestServer(t *testing.T) (*Server, func()) {
	dbPath := "test_server.db"
	store, err := db.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	srv := NewServer(store, "http://localhost:8080")
	cleanup := func() {
		store.Close()
		os.Remove(dbPath)
	}
	return srv, cleanup
}

func TestServer_Routes(t *testing.T) {
	srv, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create URL
	createReq := CreateURLRequest{TargetURL: "https://golang.org", CustomSlug: "go"}
	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest("POST", "/api/v1/urls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var resp CreateURLResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	if resp.Slug != "go" || resp.ShortURL != "http://localhost:8080/go" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// 2. Redirect
	req = httptest.NewRequest("GET", "/go", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 Found, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://golang.org" {
		t.Fatalf("expected Location https://golang.org, got %s", loc)
	}

	// 3. Stats
	req = httptest.NewRequest("GET", "/api/v1/urls/go/stats", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	var stats db.URLStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode stats: %v", err)
	}
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}

	// 4. Delete
	req = httptest.NewRequest("DELETE", "/api/v1/urls/go", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", w.Code)
	}

	// 5. Verify 404 after delete
	req = httptest.NewRequest("GET", "/go", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", w.Code)
	}
}
