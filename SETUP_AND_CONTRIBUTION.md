# Setup & Contribution Guide

This guide explains how to set up the URL Shortener Operator project from scratch, and documents the development lifecycle for making changes to either the **Backend Service (Data Plane)** or the **Kubernetes Operator (Control Plane)**.

---

## 🛠 Prerequisites

Ensure the following tools are installed on your workstation:
* **Go:** 1.25+ (`go version`)
* **Docker:** Docker Desktop or Docker engine running (`docker info`)
* **k3d:** Local Kubernetes cluster manager (`k3d version`)
* **kubectl:** Kubernetes CLI (`kubectl version --client`)
* **Kubebuilder:** v4.16+ (`kubebuilder version`)

---

## 🚀 Part 1: Setting Up the Project from Scratch (Zero to Hero)

Follow these steps to initialize a fresh local environment and get everything running:

### 1. Create the Local k3d Cluster & Namespaces
Run the setup script from the root of the repository:
```bash
./scripts/setup-cluster.sh
```
This command:
* Checks for or creates a k3d cluster named `kuber-crd-cluster`.
* Maps host port `8080` to the k3d load balancer (`--port "8080:80@loadbalancer"`).
* Creates the required namespaces: `shortener-system` (operator) and `shortener-backend` (backend service).

Verify namespaces:
```bash
kubectl get namespaces
```

---

### 2. Install the Custom Resource Definition (CRD)
Register the `URLShortener` CRD in the cluster:
```bash
kubectl apply -k operator/config/crd
```
Verify the CRD is established:
```bash
kubectl get crd urlshorteners.shortener.tapsi.cloud
```

---

### 3. Build & Deploy the Backend Service (Data Plane)
Build the image, import it into the local k3d cluster, and deploy it to `shortener-backend`:
```bash
# 1. Build the backend image
docker build -t url-shortener-backend:latest ./backend

# 2. Import the image into k3d
k3d image import url-shortener-backend:latest -c kuber-crd-cluster

# 3. Apply deployment, service, and ingress
kubectl apply -f backend/deploy/deployment.yaml

# 4. Wait for the pod to become ready
kubectl rollout status deployment/url-shortener-backend -n shortener-backend --timeout=60s
```

Test that the backend is reachable externally:
```bash
curl -s http://localhost:8080/api/v1/urls
```

---

### 4. Build & Deploy the Operator (Control Plane)
Build the operator image, import it into k3d, and apply the RBAC and manager deployment to `shortener-system`:
```bash
# 1. Build the operator image
docker build -t url-shortener-operator:latest ./operator

# 2. Import into k3d
k3d image import url-shortener-operator:latest -c kuber-crd-cluster

# 3. Deploy the operator
kubectl apply -k operator/config/default

# 4. Wait for the operator to become ready
kubectl rollout status deployment/operator-controller-manager -n shortener-system --timeout=60s
```

Check operator logs to confirm leader election and connection to the backend:
```bash
kubectl logs -n shortener-system deployment/operator-controller-manager -f
```

---

## 💻 Part 2: Developer Workflow — Modifying the Backend Service

If you are changing the URL Shortener HTTP service, SQLite database logic, or redirect engine:

### 1. Make Changes in `backend/`
* Store logic: `backend/db/db.go`
* HTTP API & redirects: `backend/server/server.go`
* Configuration & env: `backend/main.go`

### 2. Run Local Tests (TDD)
Before rebuilding images, ensure all unit tests pass:
```bash
cd backend
go test -v ./...
cd ..
```

### 3. Build, Import, and Reload in Kubernetes
Run these commands to apply your changes to the cluster:
```bash
# Rebuild the backend image
docker build -t url-shortener-backend:latest ./backend

# Import the updated image into k3d
k3d image import url-shortener-backend:latest -c kuber-crd-cluster

# Restart the deployment to pick up the new image
kubectl rollout restart deployment/url-shortener-backend -n shortener-backend

# Follow logs to verify healthy startup
kubectl logs -n shortener-backend deployment/url-shortener-backend -f
```

---

## ⚙️ Part 3: Developer Workflow — Modifying the Operator & CRD

If you are adding fields to the CRD, modifying the reconciler logic, changing polling intervals, or adjusting finalizers:

### 1. If Modifying the CRD Schema (`operator/api/v1/`)
Edit `operator/api/v1/urlshortener_types.go`. Whenever you add, change, or remove fields:
```bash
cd operator

# 1. Regenerate deepcopy code (zz_generated.deepcopy.go)
make generate

# 2. Regenerate CRD OpenAPI v3 manifests in config/crd/bases/
make manifests

# 3. Apply updated CRD to the cluster
kubectl apply -k config/crd

cd ..
```

### 2. If Modifying Controller Logic (`operator/internal/`)
* Reconcile loop & finalizer: `operator/internal/controller/urlshortener_controller.go`
* Backend HTTP client: `operator/internal/client/backend.go`
* Main initialization & flags: `operator/cmd/main.go`

Run unit tests:
```bash
cd operator
go test -v ./internal/client/...
cd ..
```

### 3. Applying Operator Changes to the Cluster

#### Option A: In-Cluster Container Deployment (Standard)
```bash
# 1. Rebuild the operator image
docker build -t url-shortener-operator:latest ./operator

# 2. Import into k3d
k3d image import url-shortener-operator:latest -c kuber-crd-cluster

# 3. Restart deployment
kubectl rollout restart deployment/operator-controller-manager -n shortener-system

# 4. Stream logs
kubectl logs -n shortener-system deployment/operator-controller-manager -f
```

#### Option B: Local Live Execution (Fastest for Rapid Debugging)
You can run the controller directly from your terminal while pointing to the k3d cluster:
```bash
# 1. Scale down the in-cluster operator to prevent dual leader conflicts:
kubectl scale deployment/operator-controller-manager -n shortener-system --replicas=0

# 2. Run the operator locally:
cd operator
BACKEND_SERVICE_URL="http://localhost:8080" go run ./cmd/main.go
```
*(When done, restore the in-cluster deployment with `kubectl scale deployment/operator-controller-manager -n shortener-system --replicas=1`)*.

---

## 🧪 Part 4: Testing & Verification Cheat Sheet

### Create a Custom Resource
```bash
kubectl apply -f operator/config/samples/shortener_v1_urlshortener.yaml
```

### Inspect Resources & Status
```bash
# Tabular view with custom printer columns:
kubectl get urlshorteners -n default

# Detailed YAML view:
kubectl get urlshorteners k8s-docs -n default -o yaml

# Check finalizers:
kubectl get urlshorteners k8s-docs -n default -o jsonpath='{.metadata.finalizers}'
```

### Test Redirection & Generate Traffic
```bash
# Test 302 redirect header:
curl -I http://localhost:8080/k8s-docs

# Follow redirect in terminal:
curl -L http://localhost:8080/k8s-docs

# Send 10 requests to test hit counter:
for i in {1..10}; do curl -s http://localhost:8080/k8s-docs > /dev/null; done

# Wait 10 seconds (polling interval) and inspect updated hit count:
kubectl get urlshorteners -n default
```

### Test Finalizer & Resource Deletion
```bash
# Delete CR:
kubectl delete urlshortener k8s-docs -n default

# Confirm slug is purged from backend (returns 404):
curl -I http://localhost:8080/k8s-docs
```

---

## 🔍 Troubleshooting

| Symptom | Cause | Solution |
|---|---|---|
| `no space left on device` during `go build` | Mac disk capacity is full. | Clean docker images with `docker system prune -f` and empty macOS Trash. |
| `operation not permitted` when running tests locally | macOS sandbox restriction on loopback ports. | Run with unsandboxed terminal or bypass permissions. |
| `CrashLoopBackOff` on operator pod | Operator cannot resolve backend service DNS. | Ensure backend is running in `shortener-backend` with `kubectl get pods -n shortener-backend`. |
| Hits not updating on CR | Operator poll interval has not elapsed or hit count hasn't changed. | The controller polls every 10s and only updates status if `hits` count changed (etcd optimization). |
| CR deletion stuck in `Terminating` | Finalizer blocked or backend service unreachable. | Check operator logs (`kubectl logs -n shortener-system deployment/operator-controller-manager`). |
