#!/usr/bin/env bash
set -euo pipefail

CLUSTER_A="cilion-a"
CLUSTER_B="cilion-b"

# KinD cluster config: single control-plane node, default CNI (kindnet) left intact
KIND_CONFIG=$(cat <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: false
EOF
)

spin_up() {
  local name=$1
  local pod_subnet=$2
  local svc_subnet=$3

  if kind get clusters 2>/dev/null | grep -q "^${name}$"; then
    echo "Cluster '${name}' already exists, skipping."
    return
  fi

  echo "Creating cluster '${name}' (pods: ${pod_subnet}, services: ${svc_subnet})..."
  echo "${KIND_CONFIG}" \
    | sed "s|networking:|networking:\n  podSubnet: \"${pod_subnet}\"\n  serviceSubnet: \"${svc_subnet}\"|" \
    | kind create cluster --name "${name}" --config -

  echo "Cluster '${name}' ready."
}

spin_up "${CLUSTER_A}" "10.10.0.0/16" "10.11.0.0/16"
spin_up "${CLUSTER_B}" "10.20.0.0/16" "10.21.0.0/16"

echo ""
echo "Both clusters are up."
echo "  kubectl --context kind-${CLUSTER_A} get nodes"
echo "  kubectl --context kind-${CLUSTER_B} get nodes"
