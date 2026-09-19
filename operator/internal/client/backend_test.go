package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackendClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/urls":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"slug":"test","shortUrl":"http://localhost:8080/test","hits":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/urls/test/stats":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"slug":"test","targetUrl":"https://example.com","hits":5}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/urls/test":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := NewBackendClient(ts.URL)
	ctx := context.Background()

	created, err := c.CreateURL(ctx, "https://example.com", "test")
	if err != nil || created.Slug != "test" {
		t.Fatalf("CreateURL failed: %v", err)
	}

	stats, err := c.GetStats(ctx, "test")
	if err != nil || stats.Hits != 5 {
		t.Fatalf("GetStats failed: %v", err)
	}

	if err := c.DeleteURL(ctx, "test"); err != nil {
		t.Fatalf("DeleteURL failed: %v", err)
	}
}
