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
