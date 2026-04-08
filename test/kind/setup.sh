#!/usr/bin/env bash
set -euo pipefail

CLUSTER_A="cilion-a"
CLUSTER_B="cilion-b"

NODE_IMAGE="kindest/node:v1.30.0"

down() {
  for cluster in "${CLUSTER_A}" "${CLUSTER_B}"; do
    if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
      echo "Deleting existing cluster '${cluster}'..."
      kind delete cluster --name "${cluster}"
    fi
  done
}

spin_up() {
  local name=$1
  local pod_subnet=$2
  local svc_subnet=$3

  if kind get clusters 2>/dev/null | grep -q "^${name}$"; then
    echo "Cluster '${name}' already exists, skipping."
    return
  fi

  echo "Creating cluster '${name}' (pods: ${pod_subnet}, services: ${svc_subnet})..."

  cat <<EOF | kind create cluster --name "${name}" --config -
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    image: ${NODE_IMAGE}
networking:
  disableDefaultCNI: true
  podSubnet: "${pod_subnet}"
  serviceSubnet: "${svc_subnet}"
EOF

  echo "Cluster '${name}' ready."
}

install_cilium() {
  local name=$1
  echo "Installing Cilium on cluster '${name}'..."
  kubectl --context "kind-${name}" create ns kube-system || true
  cilium install --context "kind-${name}" --version 1.19.2 --wait
  echo "Cilium installed on '${name}'."
}

down

spin_up "${CLUSTER_A}" "10.10.0.0/16" "10.11.0.0/16"
spin_up "${CLUSTER_B}" "10.20.0.0/16" "10.21.0.0/16"

install_cilium "${CLUSTER_A}"
install_cilium "${CLUSTER_B}"

echo ""
echo "Both clusters are up with Cilium."
echo "  kubectl --context kind-${CLUSTER_A} get nodes"
echo "  kubectl --context kind-${CLUSTER_B} get nodes"