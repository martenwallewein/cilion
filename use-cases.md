# 🌍 CilION Use Cases & Scenarios

By pushing SCION path logic down into the eBPF datapath of every worker node, CilION unlocks networking capabilities that are impossible with standard BGP, IPsec, or traditional centralized VPN gateways. 

Below are the primary sophisticated scenarios this research aims to solve.

## Scenario 1: The True Multi-Cloud SCION Mesh (The Core Uplink)
*The most powerful and practical implementation of CilION.*

**The Problem:** You have Cluster A in AWS (EU) and Cluster B in Azure (EU). You cannot install native SCION hardware in these clouds.
**The CilION Solution:** 
Cluster A is defined as its own SCION Edge AS. Instead of relying on AWS BGP to route traffic to Azure, the CilION eBPF agent establishes an **Overlay Uplink** to a local SCION Transit Provider (e.g., Anapaya Core AS). 
*   **Datapath:** The eBPF program on the AWS worker node wraps the pod's packet in a SCION header, and wraps *that* in a UDP/IP packet destined for the Anapaya SCION Router's public IP.
*   **Transit:** The Anapaya router receives it, strips the UDP header, reads the cryptographically signed SCION header, and routes it across their dedicated backbone directly to the Azure region, where it is encapsulated back into UDP for the final hop.
*   **Result:** You have fully decoupled your inter-cluster traffic from the cloud provider's global transit decisions. The cloud provider acts merely as a "first mile" local ISP.

## Scenario 2: Application-Aware WAN Routing (Per-Pod Pathing)
**The Problem:** Traditional SD-WAN gateways route traffic based on IP subnets. They do not understand Kubernetes application contexts.
**The CilION Solution:** 
Because CilION operates inside the CNI, it maps K8s pod labels directly to global physical internet paths.
*   **Pod 1 (`app: database-sync`):** The eBPF program looks up the label, assigns a highly secure, low-latency path restricted strictly to Tier-1 European backbone providers.
*   **Pod 2 (`app: video-cache`):** Living on the exact same worker node, this pod's traffic hits eBPF, which assigns a high-throughput, multi-path routing strategy that sprays packets across three different, cheaper ISPs simultaneously.
*   **Result:** Absolute granular control over WAN transit costs and performance based on Kubernetes primitives.

## Scenario 3: Cryptographic Geofencing & Data Sovereignty
**The Problem:** Standard BGP internet routing is unpredictable. Traffic between two servers in Germany might silently route through an ISP in the United States or Russia due to BGP peering economics or malicious hijacks, violating GDPR.
**The CilION Solution:**
A platform engineer defines a `ScionPathPolicy` CRD for the `finance` namespace.
*   The policy enforces that the SCION Hop Fields (the cryptographic path) *must not* contain Autonomous Systems outside of the EU.
*   If an attacker hijacks the BGP route to force traffic out of the EU, the SCION-aware routers on the internet will see a mismatch between the BGP path and the cryptographic SCION path, and immediately drop the packets.
*   **Result:** Mathematical guarantee of data geographic sovereignty at the packet level.

## Scenario 4: Active-Active Multipathing & Millisecond Failover
**The Problem:** If a major internet backbone link fails, BGP takes anywhere from 30 seconds to several minutes to reconverge. Applications experience massive packet loss.
**The CilION Solution:**
The local SCION Control Service caches multiple active, distinct internet paths between Cluster A and Cluster B inside the host's eBPF maps.
*   If eBPF telemetry detects latency or packet loss on Path 1 (e.g., a physical fiber cut at an ISP), the eBPF program **instantly swaps** to Path 2 for the very next packet. 
*   **Result:** Zero-downtime WAN failover without waiting for any control plane intervention or global BGP propagation.