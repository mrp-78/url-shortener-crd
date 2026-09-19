# URL Shortener Kubernetes Operator & CRD Design Spec

**Date:** 2026-09-19  
**Status:** Approved  
**Target Environment:** Local Kubernetes (k3d), Go 1.25+, Kubebuilder v4.16+, Docker  

---

## 1. Overview & Goals

The goal of this project is to build an educational yet production-pattern Kubernetes Operator and Custom Resource Definition (CRD) for a URL Shortener application using **Kubebuilder**.

The application decouples the **Control Plane** (the Operator managing CRDs) from the **Data Plane** (the HTTP service performing redirects and counting hits):
* Users declare shortened links declaratively via a `URLShortener` CRD.
* A backend HTTP redirect service backed by SQLite handles URL redirects and records hit counts.
* The operator controller synchronizes CR desired state with the backend service and periodically polls hit telemetry to update the CR `status.hits` and `status.phase`.
* Kubernetes finalizers ensure clean removal of data on CR deletion.

---

## 2. System Architecture

```
+-------------------------------------------------------------+
|                        k3d Cluster                          |
|                                                             |
|   +-------------------+          +-----------------------+  |
|   |  Kubernetes API   |          |  URL Shortener Svc    |  |
|   |   (CRD:           |<--Poll---|   (Go/HTTP + SQLite)  |  |
|   |    URLShortener)  |          |                       |  |
|   +---------+---------+          +-----------+-----------+  |
|             ^                                ^              |
|             | Watch / Update                 |              |
|             v                                |              |
|   +---------+---------+                      |              |
|   | URL Shortener     |                      |              |
|   | Operator          |---Register/Delete----+              |
|   | (Kubebuilder)     |                                     |
|   +-------------------+                                     |
+----------------------------------------------+--------------+
                       ^                       |
       kubectl apply   |                       | HTTP GET /:slug
                       |                       v
                Cluster User             Web Visitor
```

### Components

1. **CRD (`URLShortener`):**
   * API Group: `shortener.tapsi.cloud/v1`
   * Kind: `URLShortener`
   * Scoped to namespaces.
2. **URL Shortener Backend Service (`url-shortener-backend`):**
   * Lightweight HTTP server running inside the cluster in namespace `shortener-backend`.
   * SQLite database persisted at `/data/urls.db` (via mounted volume).
   * Exposes REST endpoints for the operator and public redirect endpoints for visitors.
   * Internal DNS: `http://url-shortener-backend.shortener-backend.svc.cluster.local:8080`.
3. **URL Shortener Operator (`url-shortener-operator`):**
   * Built with Kubebuilder.
   * Runs in the cluster in its own dedicated namespace `shortener-system` (with its own ServiceAccount, ClusterRole, and ClusterRoleBinding).
   * Reconciles `URLShortener` custom resources across all user namespaces.
   * Reaches the backend service via environment variable `BACKEND_SERVICE_URL`.

---

## 3. Custom Resource Definition (CRD) Schema

### Spec: `URLShortenerSpec`
* `targetUrl` (string, required): Full target URL. Validated by regex `^https?://.+`.
* `customSlug` (string, optional): Desired custom slug (up to 32 characters). If empty, backend generates a random 6-character slug.

### Status: `URLShortenerStatus`
* `shortUrl` (string): Full public short URL (e.g. `http://localhost:8080/my-link`).
* `slug` (string): The assigned slug identifier.
* `hits` (int64, default 0): Access counter reflecting how many times the short URL was redirected.
* `phase` (string, default "Pending"): Lifecycle phase (`Pending`, `Ready`, `Error`).
* `conditions` ([]metav1.Condition): Standard Kubernetes conditions array (`Ready`).
* `lastPolledTime` (*metav1.Time): Timestamp of the last successful metrics sync.

### Printer Columns
`kubectl get urlshorteners` outputs:
* `TARGET`: `.spec.targetUrl`
* `SHORT URL`: `.status.shortUrl`
* `HITS`: `.status.hits`
* `PHASE`: `.status.phase`
* `AGE`: `.metadata.creationTimestamp`

---

## 4. Backend Service (Data Plane) Specification

### SQLite Schema
```sql
CREATE TABLE IF NOT EXISTS urls (
    slug TEXT PRIMARY KEY,
    target_url TEXT NOT NULL,
    hits INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### API Endpoints
* **`POST /api/v1/urls`**
  * Request Body: `{"targetUrl": "https://example.com", "customSlug": "my-slug"}` (customSlug optional).
  * Response (201 Created): `{"slug": "my-slug", "shortUrl": "http://<base-url>/my-slug", "hits": 0}`.
  * Conflict (409 Conflict): Returned if `customSlug` already exists.
* **`GET /api/v1/urls/:slug/stats`**
  * Response (200 OK): `{"slug": "my-slug", "targetUrl": "https://example.com", "hits": 15}`.
  * Not Found (404 Not Found): If slug does not exist.
* **`DELETE /api/v1/urls/:slug`**
  * Response (204 No Content): Deletes the record.
* **`GET /:slug`**
  * Logic: Executes `UPDATE urls SET hits = hits + 1 WHERE slug = ?;` then redirects.
  * Response (302 Found): Redirects to `target_url`.
  * Response (404 Not Found): If slug not found.

---

## 5. Operator Reconciler (Control Plane) Specification

### Finalizer
* Constant: `shortener.tapsi.cloud/finalizer`

### Reconcile Flow
1. **Fetch Resource:** If not found, return empty result (resource has been deleted from k8s).
2. **Handle Deletion:**
   * If `DeletionTimestamp` is non-zero:
     * If finalizer exists, send `DELETE /api/v1/urls/{slug}` to backend service.
     * Remove finalizer from resource and call `r.Update()`.
     * Return `ctrl.Result{}, nil`.
3. **Ensure Finalizer:**
   * If finalizer not present, add it and call `r.Update()`.
4. **Registration (New Resource):**
   * If `status.slug` is empty:
     * Send `POST /api/v1/urls` to backend.
     * On success: Set `status.slug`, `status.shortUrl`, `status.hits = 0`, `status.phase = "Ready"`, set condition `Ready=True`. Call `r.Status().Update()`.
     * On failure: Set `status.phase = "Error"`, condition `Ready=False`. Call `r.Status().Update()`. Return `ctrl.Result{RequeueAfter: 5 * time.Second}, nil`.
5. **Periodic Telemetry Polling:**
   * If `status.slug` is present:
     * Call `GET /api/v1/urls/{slug}/stats`.
     * Compare `resp.Hits` with `status.hits`.
     * If changed: update `status.hits = resp.Hits`, `status.lastPolledTime = metav1.Now()`, call `r.Status().Update()`.
     * If unchanged: skip `r.Status().Update()` to avoid redundant etcd transactions.
   * Return `ctrl.Result{RequeueAfter: 10 * time.Second}, nil`.

---

## 6. Verification & Testing Plan

1. **Cluster Setup:** Spin up or verify local k3d cluster.
2. **Backend Deployment:** Build and deploy backend service with SQLite in the cluster, verify endpoints with `curl`.
3. **Operator Deployment:** Run operator locally (`make run`) pointing to k3d cluster for instant feedback, and then test containerized deployment.
4. **CRD Lifecycle Tests:**
   * Apply sample `URLShortener` CR.
   * Observe `kubectl get urlshorteners` transitioning from `Pending` to `Ready` with assigned `SHORT URL`.
   * Access the short URL via `curl -L http://localhost:8080/<slug>` and verify redirection.
   * Wait 10 seconds and inspect `kubectl get urlshorteners` to confirm `HITS` increments.
   * Delete the CR with `kubectl delete urlshortener <name>` and verify finalizer calls backend deletion and resource terminates cleanly.
