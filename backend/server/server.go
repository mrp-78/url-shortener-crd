package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/learn/kuber-crd/backend/db"
)

type CreateURLRequest struct {
	TargetURL  string `json:"targetUrl"`
	CustomSlug string `json:"customSlug,omitempty"`
}

type CreateURLResponse struct {
	Slug     string `json:"slug"`
	ShortURL string `json:"shortUrl"`
	Hits     int64  `json:"hits"`
}

type Server struct {
	store   *db.Store
	baseURL string
}

func NewServer(store *db.Store, baseURL string) *Server {
	return &Server{store: store, baseURL: strings.TrimRight(baseURL, "/")}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/urls", s.handleURLs)
	mux.HandleFunc("/api/v1/urls/", s.handleURLBySlug)
	mux.HandleFunc("/", s.handleRedirect)
	return mux
}

func (s *Server) handleURLs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CreateURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetURL == "" {
		http.Error(w, "invalid request body: targetUrl required", http.StatusBadRequest)
		return
	}

	slug, err := s.store.CreateURL(req.TargetURL, req.CustomSlug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateURLResponse{
		Slug:     slug,
		ShortURL: s.baseURL + "/" + slug,
		Hits:     0,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleURLBySlug(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/urls/")
	parts := strings.Split(path, "/")
	slug := parts[0]
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 2 && parts[1] == "stats" && r.Method == http.MethodGet {
		stats, err := s.store.GetStats(slug)
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
		return
	}

	if len(parts) == 1 && r.Method == http.MethodDelete {
		err := s.store.DeleteURL(slug)
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	slug := strings.Trim(r.URL.Path, "/")
	if slug == "" || strings.HasPrefix(slug, "api/") {
		http.NotFound(w, r)
		return
	}

	targetURL, _, err := s.store.GetAndIncrementHits(slug)
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, targetURL, http.StatusFound)
}
