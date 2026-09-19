# URL Shortener Kubernetes Operator & CRD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and deploy an educational, production-pattern URL Shortener Kubernetes Operator and CRD using Kubebuilder on a local k3d cluster, featuring decoupled control plane and data plane, periodic hit counter polling, and cleanup finalizers.

**Architecture:** A standalone HTTP redirect service backed by SQLite runs in the `shortener-backend` namespace handling URL redirects and hit counters. A Kubebuilder operator runs in the `shortener-system` namespace, reconciling `URLShortener` CRDs in `shortener.tapsi.cloud/v1`, registering URLs with the backend, attaching `shortener.tapsi.cloud/finalizer`, and polling hit metrics every 10 seconds to update `status.hits`.

**Tech Stack:** Go 1.25+, Kubebuilder v4.16+, Docker, k3d, Kubernetes 1.32+, `controller-runtime`, `modernc.org/sqlite` (pure Go SQLite).

**Spec:** [`docs/superpowers/specs/2026-09-19-url-shortener-operator-design.md`](file:///Users/mohammadreza/hobby/kuber-crd/docs/superpowers/specs/2026-09-19-url-shortener-operator-design.md)

## Global Constraints
* CRD API Group: `shortener.tapsi.cloud/v1`
* Kind: `URLShortener`
* Finalizer string: `shortener.tapsi.cloud/finalizer`
* Operator Namespace: `shortener-system`
* Backend Service Namespace: `shortener-backend`
* Backend In-Cluster Address: `http://url-shortener-backend.shortener-backend.svc.cluster.local:8080`
* External Port: `8080` (mapped via k3d cluster to host)

---

### Task 1: Setup Local k3d Cluster and Namespaces

**Files:**
- Create: `scripts/setup-cluster.sh`

**Interfaces:**
- Consumes: Host Docker daemon, `k3d`, `kubectl`
- Produces: Running k3d cluster named `kuber-crd-cluster` with port 8080 exposed, namespaces `shortener-system` and `shortener-backend` created.

- [ ] **Step 1: Write cluster setup script**
Create `scripts/setup-cluster.sh` with the following content:
```bash
#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="kuber-crd-cluster"

if k3d cluster list | grep -q "^${CLUSTER_NAME}"; then
    echo "Cluster ${CLUSTER_NAME} already exists."
else
    echo "Creating k3d cluster ${CLUSTER_NAME} with port 8080 mapped..."
    k3d cluster create "${CLUSTER_NAME}" \
        --port "8080:80@loadbalancer" \
        --wait
fi

echo "Creating required namespaces..."
kubectl create namespace shortener-system --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace shortener-backend --dry-run=client -o yaml | kubectl apply -f -
echo "Cluster and namespaces are ready."
```

- [ ] **Step 2: Run setup script to create cluster and verify**
Run: `chmod +x scripts/setup-cluster.sh && ./scripts/setup-cluster.sh`  
Expected: Cluster created (or verified), `kubectl get namespaces` shows `shortener-system` and `shortener-backend`.

- [ ] **Step 3: Commit**
```bash
git add scripts/setup-cluster.sh
git commit -m "chore: add k3d cluster setup script"
```

---

### Task 2: Implement URL Shortener Backend Service (Data Plane)

**Files:**
- Create: `backend/go.mod`
- Create: `backend/db/db.go`
- Create: `backend/db/db_test.go`
- Create: `backend/server/server.go`
- Create: `backend/server/server_test.go`
- Create: `backend/main.go`

**Interfaces:**
- Consumes: Standard HTTP requests and SQLite
- Produces: HTTP API on port 8080 (`POST /api/v1/urls`, `GET /api/v1/urls/:slug/stats`, `DELETE /api/v1/urls/:slug`, `GET /:slug` 302 redirect).

- [ ] **Step 1: Initialize backend Go module and write failing database tests**
Create `backend/go.mod`:
```go
module github.com/learn/kuber-crd/backend

go 1.24
```
Create `backend/db/db_test.go`:
```go
package db

import (
	"os"
	"testing"
)

func TestStore_CreateAndGet(t *testing.T) {
	dbPath := "test_urls.db"
	defer os.Remove(dbPath)

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	slug, err := store.CreateURL("https://example.com", "custom-slug")
	if err != nil {
		t.Fatalf("failed to create URL: %v", err)
	}
	if slug != "custom-slug" {
		t.Errorf("expected slug 'custom-slug', got '%s'", slug)
	}

	target, hits, err := store.GetAndIncrementHits("custom-slug")
	if err != nil {
		t.Fatalf("failed to get and increment: %v", err)
	}
	if target != "https://example.com" || hits != 1 {
		t.Errorf("unexpected target or hits: target=%s, hits=%d", target, hits)
	}

	stats, err := store.GetStats("custom-slug")
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}

	if err := store.DeleteURL("custom-slug"); err != nil {
		t.Fatalf("failed to delete URL: %v", err)
	}
	_, _, err = store.GetAndIncrementHits("custom-slug")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `cd backend && go test ./...`  
Expected: Compilation failure due to missing `db.NewStore`.

- [ ] **Step 3: Implement SQLite Store**
Add dependency `modernc.org/sqlite`:
`cd backend && go get modernc.org/sqlite`
Create `backend/db/db.go`:
```go
package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("url not found")
var ErrAlreadyExists = errors.New("slug already exists")

type URLStats struct {
	Slug      string `json:"slug"`
	TargetURL string `json:"targetUrl"`
	Hits      int64  `json:"hits"`
}

type Store struct {
	db *sql.DB
}

func NewStore(dataSourceName string) (*Store, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	schema := `CREATE TABLE IF NOT EXISTS urls (
		slug TEXT PRIMARY KEY,
		target_url TEXT NOT NULL,
		hits INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func generateSlug() (string, error) {
	bytes := make([]byte, 3)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *Store) CreateURL(targetURL, customSlug string) (string, error) {
	slug := customSlug
	if slug == "" {
		var err error
		slug, err = generateSlug()
		if err != nil {
			return "", err
		}
	}

	_, err := s.db.Exec("INSERT INTO urls (slug, target_url, hits) VALUES (?, ?, 0)", slug, targetURL)
	if err != nil {
		return "", fmt.Errorf("insert url: %w", err)
	}
	return slug, nil
}

func (s *Store) GetAndIncrementHits(slug string) (string, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	var targetURL string
	var hits int64
	err = tx.QueryRow("SELECT target_url, hits FROM urls WHERE slug = ?", slug).Scan(&targetURL, &hits)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	} else if err != nil {
		return "", 0, err
	}

	hits++
	if _, err := tx.Exec("UPDATE urls SET hits = ? WHERE slug = ?", hits, slug); err != nil {
		return "", 0, err
	}

	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return targetURL, hits, nil
}

func (s *Store) GetStats(slug string) (*URLStats, error) {
	var targetURL string
	var hits int64
	err := s.db.QueryRow("SELECT target_url, hits FROM urls WHERE slug = ?", slug).Scan(&targetURL, &hits)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &URLStats{
		Slug:      slug,
		TargetURL: targetURL,
		Hits:      hits,
	}, nil
}

func (s *Store) DeleteURL(slug string) error {
	res, err := s.db.Exec("DELETE FROM urls WHERE slug = ?", slug)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run database tests to verify they pass**
Run: `cd backend && go test ./...`  
Expected: PASS.

- [ ] **Step 5: Write failing HTTP server tests**
Create `backend/server/server_test.go`:
```go
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
}
```

- [ ] **Step 6: Implement HTTP Server and Router**
Create `backend/server/server.go`:
```go
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
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
	json.NewEncoder(w).Encode(resp)
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
		json.NewEncoder(w).Encode(stats)
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
```

- [ ] **Step 7: Implement `backend/main.go`**
```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/learn/kuber-crd/backend/db"
	"github.com/learn/kuber-crd/backend/server"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/urls.db"
	}
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	store, err := db.NewStore(dbPath)
	if err != nil {
		log.Fatalf("failed to initialize db: %v", err)
	}
	defer store.Close()

	srv := server.NewServer(store, baseURL)
	log.Printf("Starting URL Shortener Backend on :%s (Base URL: %s, DB: %s)...", port, baseURL, dbPath)
	if err := http.ListenAndServe(":"+port, srv.Router()); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
```

- [ ] **Step 8: Run all backend tests and verify**
Run: `cd backend && go test -v ./...`  
Expected: PASS for all tests.

- [ ] **Step 9: Commit**
```bash
git add backend/
git commit -m "feat: implement url shortener backend service with sqlite"
```

---

### Task 3: Build & Deploy Backend Service to Cluster

**Files:**
- Create: `backend/Dockerfile`
- Create: `deploy/backend/deployment.yaml`

**Interfaces:**
- Consumes: Backend Go application, k3d cluster
- Produces: Running Deployment and Service `url-shortener-backend` in `shortener-backend` namespace reachable at `http://url-shortener-backend.shortener-backend.svc.cluster.local:8080`.

- [ ] **Step 1: Create Dockerfile for backend service**
Create `backend/Dockerfile`:
```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o url-shortener-backend .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /app/url-shortener-backend /usr/local/bin/
RUN mkdir -p /data
EXPOSE 8080
ENV DB_PATH=/data/urls.db
ENV PORT=8080
ENV BASE_URL=http://localhost:8080
ENTRYPOINT ["/usr/local/bin/url-shortener-backend"]
```

- [ ] **Step 2: Create Kubernetes deployment manifest**
Create `deploy/backend/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: url-shortener-backend
  namespace: shortener-backend
  labels:
    app: url-shortener-backend
spec:
  replicas: 1
  selector:
    matchLabels:
      app: url-shortener-backend
  template:
    metadata:
      labels:
        app: url-shortener-backend
    spec:
      containers:
      - name: backend
        image: url-shortener-backend:latest
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 8080
        env:
        - name: DB_PATH
          value: "/data/urls.db"
        - name: BASE_URL
          value: "http://localhost:8080"
        volumeMounts:
        - name: data-volume
          mountPath: /data
      volumes:
      - name: data-volume
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: url-shortener-backend
  namespace: shortener-backend
spec:
  type: ClusterIP
  selector:
    app: url-shortener-backend
  ports:
  - name: http
    port: 8080
    targetPort: 8080
```

- [ ] **Step 3: Build image and import into k3d**
Run:
```bash
docker build -t url-shortener-backend:latest ./backend
k3d image import url-shortener-backend:latest -c kuber-crd-cluster
kubectl apply -f deploy/backend/deployment.yaml
kubectl rollout status deployment/url-shortener-backend -n shortener-backend --timeout=60s
```
Expected: Pod is Running.

- [ ] **Step 4: Test in-cluster service communication**
Run a temporary curl test inside the cluster:
```bash
kubectl run curl-test --image=curlimages/curl --rm -it --restart=Never -n shortener-system -- \
  curl -s -X POST http://url-shortener-backend.shortener-backend.svc.cluster.local:8080/api/v1/urls \
  -H "Content-Type: application/json" \
  -d '{"targetUrl": "https://example.com", "customSlug": "k8s-test"}'
```
Expected: JSON response with `{"slug":"k8s-test","shortUrl":"http://localhost:8080/k8s-test","hits":0}`.

- [ ] **Step 5: Commit**
```bash
git add backend/Dockerfile deploy/backend/
git commit -m "deploy: add dockerfile and manifests for backend in shortener-backend namespace"
```

---

### Task 4: Scaffold Kubebuilder Project and Define CRD

**Files:**
- Create: Kubebuilder project files (`PROJECT`, `Makefile`, `cmd/main.go`, etc.)
- Create: `api/v1/urlshortener_types.go`
- Generate: `config/crd/bases/shortener.tapsi.cloud_urlshorteners.yaml`

**Interfaces:**
- Consumes: Kubebuilder CLI v4.16.0
- Produces: Generated CRD in `shortener.tapsi.cloud/v1` installed on the cluster.

- [ ] **Step 1: Scaffold Kubebuilder project**
Run in root directory:
```bash
kubebuilder init --domain tapsi.cloud --repo github.com/learn/kuber-crd
kubebuilder create api --group shortener --version v1 --kind URLShortener --resource --controller
```
Expected: Directories `api/v1/`, `internal/controller/`, `config/` created.

- [ ] **Step 2: Define `URLShortener` Spec and Status in `api/v1/urlshortener_types.go`**
Replace content of `api/v1/urlshortener_types.go`:
```go
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// URLShortenerSpec defines the desired state of URLShortener
type URLShortenerSpec struct {
	// TargetURL is the full URL to redirect visitors to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://.+`
	TargetURL string `json:"targetUrl"`

	// CustomSlug is an optional custom short path identifier.
	// If omitted, the service will generate a random slug.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=32
	CustomSlug string `json:"customSlug,omitempty"`
}

// URLShortenerStatus defines the observed state of URLShortener
type URLShortenerStatus struct {
	// ShortURL is the fully qualified short URL visitors can use.
	ShortURL string `json:"shortUrl,omitempty"`

	// Slug is the unique identifier assigned by the backend service.
	Slug string `json:"slug,omitempty"`

	// Hits is the number of times this short URL has been accessed.
	// +kubebuilder:default=0
	Hits int64 `json:"hits"`

	// Phase represents the current state: Pending, Ready, Error.
	// +kubebuilder:default="Pending"
	Phase string `json:"phase,omitempty"`

	// Conditions represent observations of the resource state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastPolledTime records the last time the operator fetched hit stats.
	LastPolledTime *metav1.Time `json:"lastPolledTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type="string",JSONPath=".spec.targetUrl"
// +kubebuilder:printcolumn:name="Short URL",type="string",JSONPath=".status.shortUrl"
// +kubebuilder:printcolumn:name="Hits",type="integer",JSONPath=".status.hits"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// URLShortener is the Schema for the urlshorteners API
type URLShortener struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   URLShortenerSpec   `json:"spec,omitempty"`
	Status URLShortenerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// URLShortenerList contains a list of URLShortener
type URLShortenerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []URLShortener `json:"items"`
}

func init() {
	SchemeBuilder.Register(&URLShortener{}, &URLShortenerList{})
}
```

- [ ] **Step 3: Generate CRD manifests and install onto k3d**
Run:
```bash
make manifests
make install
kubectl get crd urlshorteners.shortener.tapsi.cloud
```
Expected: `urlshorteners.shortener.tapsi.cloud` is registered and established.

- [ ] **Step 4: Commit**
```bash
git add PROJECT Makefile go.mod go.sum api/ config/ cmd/ internal/
git commit -m "feat: scaffold kubebuilder operator and install URLShortener CRD"
```

---

### Task 5: Implement Backend Client and Operator Reconciler Logic

**Files:**
- Create: `internal/client/backend.go`
- Create: `internal/client/backend_test.go`
- Modify: `internal/controller/urlshortener_controller.go`
- Test: `internal/controller/urlshortener_controller_test.go`

**Interfaces:**
- Consumes: `shortenerv1.URLShortener`, Backend HTTP API
- Produces: Reconciled CRD status (`shortUrl`, `slug`, `hits`, `phase`), Finalizer `shortener.tapsi.cloud/finalizer`.

- [ ] **Step 1: Write backend HTTP client with unit tests**
Create `internal/client/backend.go`:
```go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type CreateRequest struct {
	TargetURL  string `json:"targetUrl"`
	CustomSlug string `json:"customSlug,omitempty"`
}

type CreateResponse struct {
	Slug     string `json:"slug"`
	ShortURL string `json:"shortUrl"`
	Hits     int64  `json:"hits"`
}

type StatsResponse struct {
	Slug      string `json:"slug"`
	TargetURL string `json:"targetUrl"`
	Hits      int64  `json:"hits"`
}

type BackendClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewBackendClient(baseURL string) *BackendClient {
	return &BackendClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *BackendClient) CreateURL(ctx context.Context, targetURL, customSlug string) (*CreateResponse, error) {
	reqBody, err := json.Marshal(CreateRequest{TargetURL: targetURL, CustomSlug: customSlug})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/urls", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var result CreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *BackendClient) GetStats(ctx context.Context, slug string) (*StatsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/urls/%s/stats", c.baseURL, slug), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var stats StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (c *BackendClient) DeleteURL(ctx context.Context, slug string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/api/v1/urls/%s", c.baseURL, slug), nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http delete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 2: Write tests for BackendClient**
Create `internal/client/backend_test.go`:
```go
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
```

- [ ] **Step 3: Run client test to verify it passes**
Run: `go test -v ./internal/client/...`  
Expected: PASS.

- [ ] **Step 4: Implement Reconciler logic in `internal/controller/urlshortener_controller.go`**
Replace content of `internal/controller/urlshortener_controller.go`:
```go
package controller

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	shortenerv1 "github.com/learn/kuber-crd/api/v1"
	shortenerclient "github.com/learn/kuber-crd/internal/client"
)

const (
	URLShortenerFinalizer = "shortener.tapsi.cloud/finalizer"
	DefaultPollInterval   = 10 * time.Second
)

// URLShortenerReconciler reconciles a URLShortener object
type URLShortenerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	BackendClient *shortenerclient.BackendClient
	PollInterval  time.Duration
}

// +kubebuilder:rbac:groups=shortener.tapsi.cloud,resources=urlshorteners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=shortener.tapsi.cloud,resources=urlshorteners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=shortener.tapsi.cloud,resources=urlshorteners/finalizers,verbs=update

func (r *URLShortenerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var urlShortener shortenerv1.URLShortener
	if err := r.Get(ctx, req.NamespacedName, &urlShortener); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	pollInterval := r.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}

	// 1. Handle Finalizer & Deletion
	if !urlShortener.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&urlShortener, URLShortenerFinalizer) {
			logger.Info("Deleting short URL from backend service", "slug", urlShortener.Status.Slug)
			if urlShortener.Status.Slug != "" && r.BackendClient != nil {
				if err := r.BackendClient.DeleteURL(ctx, urlShortener.Status.Slug); err != nil {
					logger.Error(err, "Failed to delete URL from backend service")
					return ctrl.Result{RequeueAfter: 5 * time.Second}, err
				}
			}
			controllerutil.RemoveFinalizer(&urlShortener, URLShortenerFinalizer)
			if err := r.Update(ctx, &urlShortener); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// 2. Ensure Finalizer is present
	if !controllerutil.ContainsFinalizer(&urlShortener, URLShortenerFinalizer) {
		controllerutil.AddFinalizer(&urlShortener, URLShortenerFinalizer)
		if err := r.Update(ctx, &urlShortener); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 3. Register URL if not yet assigned a slug
	if urlShortener.Status.Slug == "" {
		logger.Info("Registering URL with backend service", "targetUrl", urlShortener.Spec.TargetURL)
		resp, err := r.BackendClient.CreateURL(ctx, urlShortener.Spec.TargetURL, urlShortener.Spec.CustomSlug)
		if err != nil {
			logger.Error(err, "Failed to register URL with backend service")
			urlShortener.Status.Phase = "Error"
			meta.SetStatusCondition(&urlShortener.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "BackendRegistrationFailed",
				Message: err.Error(),
			})
			_ = r.Status().Update(ctx, &urlShortener)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		urlShortener.Status.Slug = resp.Slug
		urlShortener.Status.ShortURL = resp.ShortURL
		urlShortener.Status.Hits = resp.Hits
		urlShortener.Status.Phase = "Ready"
		meta.SetStatusCondition(&urlShortener.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "Registered",
			Message: "Successfully registered URL with backend service",
		})
		if err := r.Status().Update(ctx, &urlShortener); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil
	}

	// 4. Poll Hits Telemetry
	stats, err := r.BackendClient.GetStats(ctx, urlShortener.Status.Slug)
	if err != nil {
		logger.Error(err, "Failed to fetch stats from backend service", "slug", urlShortener.Status.Slug)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Optimization: Only update status if hits changed
	if stats.Hits != urlShortener.Status.Hits {
		logger.Info("Hits changed, updating status", "oldHits", urlShortener.Status.Hits, "newHits", stats.Hits)
		urlShortener.Status.Hits = stats.Hits
		now := metav1.Now()
		urlShortener.Status.LastPolledTime = &now
		if err := r.Status().Update(ctx, &urlShortener); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *URLShortenerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&shortenerv1.URLShortener{}).
		Named("urlshortener").
		Complete(r)
}
```

- [ ] **Step 5: Wire backend client into `cmd/main.go`**
In `cmd/main.go`, read `BACKEND_SERVICE_URL` from env (defaulting to `http://url-shortener-backend.shortener-backend.svc.cluster.local:8080`) and pass `BackendClient` to `URLShortenerReconciler`.

- [ ] **Step 6: Run tests and verify compilation**
Run: `go test ./...`  
Expected: PASS.

- [ ] **Step 7: Commit**
```bash
git add internal/client/ internal/controller/ cmd/main.go
git commit -m "feat: implement reconciler with registration, polling, and finalizer"
```

---

### Task 6: Deploy Operator to `shortener-system` & End-to-End Verification

**Files:**
- Create: `config/samples/shortener_v1_urlshortener.yaml`
- Modify: `config/default/kustomization.yaml` (set namespace to `shortener-system`)
- Create: `config/manager/manager_env_patch.yaml`

**Interfaces:**
- Consumes: Built operator image `url-shortener-operator:latest`, k3d cluster
- Produces: Verified end-to-end flow with CR creation, URL redirect, hit counter increment in status, and finalizer cleanup.

- [ ] **Step 1: Configure operator deployment to use namespace `shortener-system` and inject `BACKEND_SERVICE_URL`**
In `config/default/kustomization.yaml`, ensure `namespace: shortener-system`.
Add environment variable `BACKEND_SERVICE_URL` to the manager pod spec.

- [ ] **Step 2: Build operator container image and import into k3d**
Run:
```bash
docker build -t url-shortener-operator:latest .
k3d image import url-shortener-operator:latest -c kuber-crd-cluster
```

- [ ] **Step 3: Deploy operator manifests**
Run:
```bash
make deploy IMG=url-shortener-operator:latest
kubectl rollout status deployment/kuber-crd-controller-manager -n shortener-system --timeout=90s
```
Expected: Operator pod running in `shortener-system`.

- [ ] **Step 4: Create sample URLShortener CR**
Create `config/samples/shortener_v1_urlshortener.yaml`:
```yaml
apiVersion: shortener.tapsi.cloud/v1
kind: URLShortener
metadata:
  name: doc-link
  namespace: default
spec:
  targetUrl: "https://kubernetes.io/docs/home/"
  customSlug: "k8s-docs"
```
Apply: `kubectl apply -f config/samples/shortener_v1_urlshortener.yaml`

- [ ] **Step 5: Verify status and printer columns**
Run: `kubectl get urlshorteners -n default`  
Expected:
```
NAME       TARGET                              SHORT URL                       HITS   PHASE   AGE
doc-link   https://kubernetes.io/docs/home/    http://localhost:8080/k8s-docs   0      Ready   5s
```

- [ ] **Step 6: Test URL redirection and hit counter**
Run:
```bash
curl -I http://localhost:8080/k8s-docs
```
Expected: `HTTP/1.1 302 Found` with `Location: https://kubernetes.io/docs/home/`.

Make 3 more requests:
```bash
curl -s http://localhost:8080/k8s-docs > /dev/null
curl -s http://localhost:8080/k8s-docs > /dev/null
curl -s http://localhost:8080/k8s-docs > /dev/null
```
Wait 10 seconds for the operator poll cycle.
Run: `kubectl get urlshorteners -n default`  
Expected: `HITS` shows `4`.

- [ ] **Step 7: Test Finalizer cleanup**
Run: `kubectl delete urlshortener doc-link -n default`  
Expected: Command completes cleanly, CR is removed.
Verify slug is deleted from backend:
```bash
curl -I http://localhost:8080/k8s-docs
```
Expected: `HTTP/1.1 404 Not Found`.

- [ ] **Step 8: Commit all configuration and verification files**
```bash
git add config/
git commit -m "feat: complete deployment and end-to-end verification configuration"
```
