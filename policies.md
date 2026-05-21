# 🛠️ CilION Architecture & Policy Specification

## 1. System Architecture

CilION relies on a strict separation of concerns, splitting the control plane to prevent "thundering herd" API calls, and positioning its eBPF datapath directly beneath the primary CNI (Cilium) as an **Underlay Interceptor**. The cluster acts as a single SCION Edge Autonomous System (Stub AS).

### 1.1. The Control Plane (Operator & Agent)
Following the "Cilium Way," the control plane is split into two distinct components synchronized entirely via Kubernetes CRDs. **The components never talk directly to each other (no custom gRPC or REST APIs).** 

*   **The Global Controller (Deployment):** The "brain" of the cluster. It watches user-created intent CRDs (`ScionPathPolicy`). It is the *only* component that communicates with the global SCION Control Service (CS). It fetches valid cryptographic paths and saves them back to the K8s API.
*   **The Local Node Agent (Privileged DaemonSet):** Because eBPF programs live in the Linux Kernel of a specific machine, the Agent must run on *every single worker node*. It monitors local Pod scheduling and watches for computed paths. It has zero knowledge of the external SCION network; it reads raw byte arrays from the K8s API and injects them into the host OS's eBPF maps.

### 1.2. The Data Plane (Native Routing & eBPF)
To enable Application-Aware WAN routing without fighting the primary CNI, CilION requires the cluster to run in **Native Routing Mode** (e.g., Cilium with VXLAN/Geneve disabled).

*   **The Handoff:** Cilium processes local L3-L7 Network Policies at the Pod's `veth` interface and routes cross-cluster traffic out to the Linux kernel in *cleartext* (unencapsulated).
*   **Egress (TC eXpress on Physical NIC):** CilION intercepts standard IP packets exactly as they enter the physical interface (e.g., `eth0`). It performs a map lookup on the source Pod IP, applies the SCION-over-UDP encapsulation, and routes it to the transit provider.
*   **Ingress (XDP Hook on Physical NIC):** Intercepts incoming SCION-over-UDP WAN traffic at the NIC driver level. It verifies the SCION MAC, strips the SCION headers completely, and passes the pristine inner IP packet up the Linux stack, where Cilium unknowingly takes over for local delivery.

---

## 2. The "Intent vs. State" Synchronization & Deduplication

To make the central Controller and the local Agents sync perfectly and scale massively, CilION decouples User Intent from Network State to prevent the **N * M Scaling Problem**.

If 1,000 Pod-specific policies all request a "low-latency" path to the same destination AS, generating 1,000 identical computed paths would crash the K8s API server and overload the eBPF maps. Instead, the Global Controller acts as a deduplication engine.

1.  **`ScionPathPolicy` (User Intent):** Created by the User. Example: *"Frontend pods require a low-latency path to AS 64:0:anap."*
2.  **`ScionComputedPath` (System State / Shared Route):** Created by the **Controller**. It represents a *mathematical network path*, not a policy. Example: `dst-64-0-anap-lowlatency`.

### The Synchronization Flow
1. **Aggregate & Deduplicate:** The Global Controller wakes up, evaluates all User Policies, and groups them by `Destination AS + Strategy`.
2. **Fetch:** It asks the external SCION Control Service for the path for `AS 64:0:anap` (Low Latency). 
3. **Publish:** It creates/updates a single, shared `ScionComputedPath` CRD (e.g., `dst-64-0-anap-lowlatency`).
4. **Inject:** The Local Node Agent watches both CRDs. It sees that Pod A belongs to Policy X, which requires Path Y. It injects the bytes of Path Y into the Linux Kernel via eBPF.

*(Note: Because `ScionComputedPaths` are shared, the Global Controller uses internal reference counting for garbage collection, rather than strict Kubernetes `OwnerReferences`, ensuring a path is only deleted when 0 policies require it).*

---

## 3. Policy Hierarchy & Precedence

CilION supports broad cluster rules and granular pod-level overrides. The resolution rule is: **The most specific policy wins.**

### 3.1. The Design Matrix
| Level | K8s Construct | Use Case |
| :--- | :--- | :--- |
| **1. Cluster-Wide** | `ClusterScionPathPolicy` | "Send ALL backup jobs across all namespaces over high-throughput paths." |
| **2. Namespace** | `ScionPathPolicy` | "Send `frontend` pods in the `production` namespace over low-latency paths." |
| **3. Per-Pod** | Pod Annotations | `cilion.io/force-policy: dark-fiber-path` (Used for debugging/overrides). |

### 3.2. Agent Policy Resolution & Map Injection
The eBPF datapath does not understand namespaces, labels, or K8s CRDs. It only understands `Pod IP -> Path ID`. The Go DaemonSet agent resolves the K8s hierarchy and maps the Pod directly to the underlying shared Network Path:

```go
func (r *CilionAgentReconciler) EvaluateEffectivePath(ctx context.Context, pod *corev1.Pod) (PathID uint32, err error) {
	// 1. Resolve which ScionPathPolicy applies to this Pod (Most Specific Wins)
	var activePolicy *cilionv1alpha1.ScionPathPolicy
	
	if override, exists := pod.Annotations["cilion.io/force-policy"]; exists {
		activePolicy = r.getPolicy(override)
	} else if nsPol := r.getNamespacePolicyMatch(pod); nsPol != nil {
		activePolicy = nsPol
	} else if globalPol := r.getGlobalPolicyMatch(pod); globalPol != nil {
		activePolicy = globalPol
	}

	if activePolicy == nil {
		return 0, nil // 0 = bypass SCION, use default host BGP
	}

	// 2. Map the Policy to the Shared Computed Path
	// e.g., "AS-64-0-ANAP" + "LowestLatency"
	computedPathName := generateSharedPathName(activePolicy.Spec.DestinationAS, activePolicy.Spec.Strategy)
	
	// 3. Return a unique uint32 hash of the path name for the eBPF map
	return hashToUint32(computedPathName), nil 
}
```

---

## 4. The Network Datapath (SCION Overlay)

### 4.1. The Pod Lifecycle Trigger (IP Assignment vs. Readiness)
The Agent must **not** wait for a Pod to become "Ready". If it waits, early application traffic will leak out over default BGP routing before the eBPF rules are injected. 

The Agent watches for the exact millisecond the local CNI assigns a `PodIP`:

```go
podNetworkReadyPredicate := predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldPod := e.ObjectOld.(*corev1.Pod)
		newPod := e.ObjectNew.(*corev1.Pod)
		
		// Trigger 1: Pod just got its IP address assigned by the local CNI
		if oldPod.Status.PodIP == "" && newPod.Status.PodIP != "" { return true }
		
		// Trigger 2: Pod labels changed (might affect routing policy)
		if !reflect.DeepEqual(oldPod.Labels, newPod.Labels) { return true }
		return false
	},
}
```

*Note: When a Pod is deleted (`!pod.ObjectMeta.DeletionTimestamp.IsZero()`), the Agent must delete the Pod IP from the eBPF map to prevent routing leaks if the IP is later reused.*

### 4.2. Egress Packet Walkthrough
1. **Pod Payload:** Pod A (`10.244.1.5`) sends a standard TCP packet to remote Pod B.
2. **Cilium Processing:** Cilium evaluates local Network Policies, allows the packet, and hands it to the Linux routing table unencrypted.
3. **eBPF Underlay Interception:** The packet hits `eth0`. CilION's `tc` program catches it.
4. **Encapsulation:** The eBPF program uses `bpf_skb_adjust_room()` to push new headers:
    ```text
    [ Outer IPv4 (Host Node -> Transit ISP IP) ] 
    [ Outer UDP (Port 30041) ] 
    [ SCION Common Header ] 
    [ SCION Path Header (Cryptographic Hop Fields) ] 
    [ Original Cleartext IP Packet (Pod A -> Pod B) ]
    ```
5. **Transit Hand-off:** Leaves the host node. Outer IPv4 routes over standard BGP *only until* it hits the SCION Transit Provider (Core AS), which then forwards based purely on the SCION Path Header.

### 4.3. eBPF Map Structures
Because paths are deduplicated, the cache map is incredibly memory efficient. A cluster with 10,000 Pods and 50 SCION destinations only requires 50 entries in the `scion_path_cache`.

```c
// Maps Local Pod IPs directly to a Shared Path ID (Decoupled from K8s Policies)
struct bpf_map_def SEC("maps") pod_path_map = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(__u32),   // Pod IPv4 Address (e.g., 10.244.1.5)
    .value_size = sizeof(__u32), // Path ID (Hash of destination+strategy)
    .max_entries = 10000,
};

// Caches pre-computed SCION Path Headers generated by the Global Controller
struct scion_path_entry {
    __u32 next_hop_ip;      // IP of the SCION Core AS Router
    __u16 path_len;
    __u8  hop_fields[256];  // The exact SCION path
};

struct bpf_map_def SEC("maps") scion_path_cache = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(__u32), // Path ID 
    .value_size = sizeof(struct scion_path_entry),
    .max_entries = 1024,       // Highly efficient due to deduplication
};
```

---

## 5. Controller-Runtime & Reconciler Best Practices

CilION is built on highly optimized `controller-runtime` state machines to protect the K8s API server and eBPF maps.

### 5.1. Local Node Cache Filtering (DaemonSet Protection)
Because the Agent runs as a DaemonSet on every node, Node A must not process or inject rules for Pods living on Node B. The controller `Manager` is configured to **only cache Pods assigned to its own physical node**.

```go
// In main.go (Agent)
nodeName := os.Getenv("NODE_NAME") // Passed via K8s Downward API

mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    Scheme: scheme,
    Cache: cache.Options{
        ByObject: map[client.Object]cache.ByObject{
            // Restrict the Pod cache so this Agent ONLY sees local Pods
            &corev1.Pod{}: {
                Field: fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName}),
            },
        },
    },
})
```

### 5.2. Core Reconciler Mechanics
*   **Level-Triggered Amnesia:** Reconcilers do not process "Events". They process the *current state of the world*. If the Agent wakes up, it reads the current Pod IPs and current CRDs from the local memory cache and enforces that state in the kernel.
*   **Idempotency & Deduplication:** The controller work queue automatically drops redundant requests. If a policy is updated 10 times in one second, the reconciler only executes once. eBPF map injections use `Put` (Create or Update), making it safe to execute repeatedly.
*   **Graceful Clean-up via Finalizers:** Deleting a `ScionPathPolicy` does not immediately remove it from K8s. A Finalizer blocks deletion until the Agent successfully unloads the associated eBPF maps from the kernel, guaranteeing zero orphaned kernel rules.
*   **Strict Spec/Status Separation:** Controllers never mutate `.Spec`. They only update `.Status` (using `r.Status().Update()`) with explicit K8s `metav1.Condition` types to communicate readiness and errors back to the user.

---

## 6. Custom Resource Definitions (CRDs) Examples

### 6.1. ScionPathPolicy (User Intent)
```yaml
apiVersion: cilion.io/v1alpha1
kind: ScionPathPolicy
metadata:
  name: gdpr-strict-path
  namespace: data-lake
spec:
  podSelector:
    matchLabels:
      data: sensitive
  pathConstraints:
    destinationAS: "64:0:anap"
    requireISDs: [ 42, 64 ]       # EU Isolation Domains only
    strategy: "LowestLatency"     # Instructs Controller to select the fastest path
```

### 6.2. ScionComputedPath (System State - Internal)
*Automatically generated by the Global Controller. Shared across multiple user policies to conserve API server load.*
```yaml
apiVersion: cilion.io/v1alpha1
kind: ScionComputedPath
metadata:
  # Named by Intent, not by Policy. 
  name: dst-64-0-anap-lowestlatency 
spec:
  destinationAS: "64:0:anap"
  strategy: "LowestLatency"
  nextHopIP: "198.51.100.42"
  expirationTime: "2026-05-15T18:00:00Z"
  hopFields: "base64-encoded-raw-bytes-for-ebpf-injection..."
status:
  # The Global Controller tracks references. When this hits 0, the CRD is deleted.
  activePolicyReferences: 142 
```