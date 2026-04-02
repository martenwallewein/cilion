# 🛠️ Architecture & Technical Specification

## 1. System Architecture
CilION adopts a "DaemonSet + Kernel" architecture, treating the entire Kubernetes cluster as a single SCION **Edge Autonomous System (Stub AS)**, fully connected to the global SCION network.

### 1.1. The Control Plane (CilION Agent)
Deployed as a privileged DaemonSet on every worker node.
*   Embeds a SCION Control Service (CS) client and a Cilium-like policy watcher.
*   Communicates with the global SCION network to fetch valid cryptographic paths (Hop Fields) to remote K8s clusters.
*   Monitors Kubernetes CRDs (`ScionLink`, `ScionPathPolicy`) and Pod metadata.
*   Compiles routing decisions and cryptographic keys, injecting them into the host OS's eBPF Maps.

### 1.2. The Data Plane (eBPF Kernel Space)
The actual routing work is performed purely in the Linux kernel via eBPF.
*   **Egress (TC Hook):** Intercepts standard IP packets leaving a Pod's `veth` interface. Performs map lookups to determine the remote cluster, applies the SCION-over-UDP encapsulation, and calculates the cryptographic MAC.
*   **Ingress (XDP Hook):** For maximum performance, incoming WAN traffic is intercepted at the NIC driver level. The eBPF program verifies the SCION MAC, strips the SCION/UDP headers, and passes the inner IP packet up the local K8s networking stack.

---

## 2. The Network Datapath (SCION Overlay)

Because public cloud providers do not yet route native SCION Ethernet frames, CilION utilizes a **SCION-over-UDP/IP Overlay** to reach the SCION network.

### Packet Walkthrough (Egress)
1.  **Pod Payload:** Pod `10.0.1.5` sends a standard TCP packet to remote Pod `10.0.2.10`.
2.  **eBPF Interception:** The `tc` program intercepts the packet. A map lookup determines this destination belongs to Remote Cluster B.
3.  **Path Selection:** The eBPF program selects an active SCION Path.
4.  **Encapsulation:** The eBPF program uses `bpf_skb_adjust_room()` to push new headers onto the packet:
    ```text
    [ Outer IPv4 (Host -> Transit ISP) ] 
    [ Outer UDP (Port 30041) ] 
    [ SCION Common Header ] 
    [ SCION Path Header (Cryptographic Hop Fields) ] 
    [ SCION E2E Extension (Cilium Security Identity) ] 
    [ Original IP Packet ]
    ```
5.  **Transit Hand-off:** The packet is sent to the local cloud provider's default gateway. The Outer IPv4 ensures it routes over standard BGP *only until* it hits the SCION Transit Provider (Core AS). 
6.  **Core Routing:** The Transit Provider receives it, ignores BGP, and forwards the packet globally based purely on the inner SCION Path Header.

---

## 3. eBPF Map Structures

To perform encapsulation at line rate without user-space context switching, CilION relies on highly optimized eBPF maps.

```c
// Maps Pod IPs to specific routing constraint profiles (App-Aware Routing)
struct bpf_map_def SEC("maps") pod_policy_map = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(__u32),   // Pod IPv4 Address
    .value_size = sizeof(__u32), // Policy ID
    .max_entries = 10000,
};

// Caches pre-computed SCION Path Headers and Next-Hop IPs
struct scion_path_entry {
    __u32 next_hop_ip;      // IP of the SCION Core AS Router
    __u8  next_hop_mac[6];  // Next hop MAC (for L2 underlays)
    __u16 path_len;
    __u8  hop_fields[256];  // The exact SCION path
    __u8  mac_key[16];      // Symmetric key for fast AES-MAC validation
};

struct bpf_map_def SEC("maps") scion_path_cache = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(__u32), // Remote Cluster AS / Policy ID
    .value_size = sizeof(struct scion_path_entry),
    .max_entries = 1024,
};
```

---

## 4. Custom Resource Definitions (CRDs)

Administrators declare global WAN topologies using K8s-native manifests.

### 4.1. ScionLink (The Core AS Uplink)
Defines how the cluster's worker nodes connect to the SCION Internet.
```yaml
apiVersion: cilion.io/v1alpha1
kind: ScionLink
metadata:
  name: anapaya-transit-uplink
spec:
  linkType: "Parent"         # Denotes an uplink to a Core AS
  remoteAS: "64:0:anap"      # The Transit Provider's AS Number
  underlay:
    type: "UDP/IPv4"
    remoteIP: "198.51.100.42" # Public IP of the Transit Provider's SCION Gateway
    remotePort: 30041
```

### 4.2. ScionPathPolicy
Maps Kubernetes labels to global routing logic.
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
    requireISDs: [ 42, 64 ]       # EU Isolation Domains only
    avoidTransitASes: [ "ff00:0:bad1" ]
    strategy: "LowestLatency"     # Instructs eBPF to monitor and select the fastest path
```

---

## 5. Research Hurdles & Implementation Challenges

Building CilION requires solving several edge-case challenges in eBPF and kernel programming:
1.  **eBPF Cryptography:** SCION relies on computing Message Authentication Codes (MACs) for hop validation. Implementing fast, secure AES-MAC generation within the strict instruction limits of the eBPF Verifier requires either clever use of the Linux Kernel Crypto API (`bpf_crypto_ctx`) or userspace pre-computation.
2.  **MTU Management:** Encapsulating SCION inside UDP/IP adds significant header overhead. The datapath must dynamically clamp TCP MSS (Maximum Segment Size) in eBPF to prevent fragmentation across the WAN.
3.  **NAT Traversal:** If K8s worker nodes reside in private cloud subnets, the CilION agent must actively maintain UDP hole-punching/keep-alives so returning SCION packets from the Transit Provider can reach the ingress XDP hook.