# 🧠 Project CilION: Architectural Discussions & Decisions

This document summarizes the core architectural discussions, evaluations, and decisions that shape the design of **CilION**, an eBPF-powered, SCION-integrated Kubernetes network dataplane.

---

## 1. Topological Mapping: K8s to SCION
**Question:** How should Kubernetes boundaries map to SCION routing boundaries? Should a Cluster be an AS, or should every Node be an AS?
**Decision:** **Cluster = Edge AS (Stub AS).**
*   Treating the entire cluster as a single Autonomous System aligns with standard administrative boundaries and allows for a unified Trust Root Configuration (TRC). 
*   Nodes do not need individual ASNs. Instead, every worker node acts as a **distributed SCION End Host and Border Router (BR)**, eliminating centralized gateways.
*   The broader organization (e.g., a fleet of multi-cloud clusters) can be grouped logically into a single SCION Isolation Domain (ISD).

## 2. Gateway vs. Distributed CNI
**Question:** Should we just build a centralized SCION IP Gateway (SIG) at the cluster border, or push SCION down into the CNI/eBPF layer on every node?
**Decision:** **Distributed eBPF CNI ("Aiming for the Stars").**
*   While a centralized gateway is easier to build, it introduces a massive chokepoint (hairpinning) and fundamentally lacks Kubernetes context.
*   By integrating SCION directly into the eBPF datapath of every worker node, we unlock **Application-Aware WAN Routing**. Because the eBPF program executes at the pod's virtual interface, it can apply physical internet routing paths based on Kubernetes Pod Labels (e.g., `app: database` gets a dedicated low-latency path, `app: frontend` gets a standard multipath internet route).

## 3. The Datapath: L2 vs. BGP Underlays
**Question:** How secure is SCION if the underlying transport between clusters still relies on BGP (the public internet)?
**Decision:** **Hybrid Datapath (SCION-over-L2 and SCION-over-UDP).**
*   **Native L2:** For clusters connected via Dark Fiber or Cloud Direct Connects, eBPF encapsulates packets in SCION-over-Ethernet. This removes IP entirely, creating absolute immunity to BGP hijacking and providing zero-overhead, bare-metal performance.
*   **SCION-over-UDP:** For public internet traversal, eBPF wraps SCION in UDP/IP. While subject to BGP link failures, the SCION cryptographic MAC ensures BGP hijacks cannot alter the path or decrypt/inject traffic. Multipathing allows instant failover if a BGP route drops.

## 4. The Cloud Transit Dilemma
**Question:** Public cloud providers (AWS, Azure) don't run SCION. How do we interconnect clusters without relying on them?
**Decision:** **The Overlay Uplink to Core ASes.**
*   The Kubernetes cluster acts as a fully-fledged Edge AS. 
*   The eBPF datapath creates a UDP overlay explicitly destined for the public IP of a nearby **SCION Transit Provider (Core AS)**, such as Anapaya.
*   AWS/Azure are relegated to "first-mile" local ISPs. Once the packet hits the Transit Provider, the intact SCION header dictates the rest of the global journey across dedicated SCION backbones.

## 5. End Host vs. Border Router Logic
**Realization:** In SCION, the path is immutable once it leaves the source.
*   The worker node eBPF program acts primarily as the **End Host**: It looks up the App Label, selects the path, constructs the SCION header, and computes the source MAC.
*   In a hyper-converged research model, the eBPF program also performs the **Overlay Egress Border Router** duties—wrapping the packet in UDP and firing it at the Transit Provider. This avoids needing a separate Border Router VM outside the cluster.

## 6. Implementation Strategy: Standalone vs. Fork
**Question:** Should we fork Cilium, or build a standalone agent?
**Decision:** **Standalone Agent using `cilium/ebpf`.**
*   Forking an enterprise CNI is a maintenance nightmare and distracts from the core research.
*   CilION will run as a standalone privileged `DaemonSet`. It can run *on top* of standard CNIs (like Flannel or basic Cilium). The base CNI handles local pod IPAM, while the CilION agent attaches eBPF programs to intercept and hijack cross-cluster traffic.

## 7. eBPF Attachment Lifecycle
**Question:** How does a containerized Go service manage kernel eBPF programs?
**Decision:** **DaemonSet Orchestration.**
*   The CilION Go Agent runs with `hostNetwork: true` and `CAP_BPF`.
*   It mounts `/sys/fs/bpf` to pin maps.
*   *Attachment Strategy:* The agent watches the K8s API. For a simplified research approach, it attaches the `TC` (Egress) and `XDP` (Ingress) programs to the host's main physical interface (`eth0`). For a strict zero-trust approach, it dynamically attaches `TC` to individual pod `veth` interfaces as they are scheduled.

## 8. Conclusion: What is CilION?
Strictly speaking, CilION exceeds the boundaries of a standard Container Network Interface (CNI). It is a **Kubernetes Software-Defined WAN (SD-WAN)** and a **Global Network Dataplane**. It bridges the micro-world of local Kubernetes network policies with the macro-world of cryptographic, path-aware global internet routing.