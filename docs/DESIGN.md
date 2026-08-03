# Design illustration

```mermaid
flowchart TD
  subgraph VM["QEMU ttsim VM"]
    direction TB
    H["Host/guest hardware surface\n/dev/tenstorrent, /sys/class/tenstorrent, PCI sysfs"]
    TTK["tt-kmd"]
  end

  subgraph Node["Node-side agents"]
    RA["Resource Allocator Agent\n(dra driver)"]
    TD["Topology Discovery Agent"]
    HD["Hardware Janitor\npre-flight + health checks"]
    Kubelet["Kubelet plugin\nCDI + allocation validation"]
  end

  subgraph Control["Control plane"]
    API["Kubernetes API Server\n(ResourceClasses, ResourceClaims, ResourceSlices,\nTopology/Fabric CRDs)"]
    CTRL["TenstorrentWorkload Controller"]
    Sched["Kubernetes Scheduler + DRA"]
  end

  User["Developer / CI"] -->|submits\nTenstorrentWorkload or Pod with DRA claim| CTRL
  H --> TTK --> RA
  H --> TD

  RA -->|publishes| API
  TD -->|publishes TenstorrentNodeTopology| API
  API -->|cluster-wide merge| CTRL
  CTRL -->|validates fabric/ring adjacency| API
  CTRL -->|creates| RC["ResourceClaims"]
  CTRL -->|creates rank-bound Pods| Pods["Rank Pods"]
  RC --> Sched
  Pods -->|requested devices| Sched
  Sched -->|binding decision| Kubelet
  Kubelet -->|exposes /dev paths through CDI| RA
  Kubelet -->|starts pod| Container["Workload container\n(executing TT workload)"]
  Container -->|faults / health events| HD
  HD -->|cordon/taint + remediation| API
  HD -->|device scrub before start| Kubelet

  CTRL -->|pins assignment per fabric generation| API
```

## How the system operates

1. The Resource Allocator Agent discovers cards from `tt-kmd` and publishes whole-card inventory as `ResourceSlices` in Kubernetes (health, PCI/NUMA context, capacity, identity, and class attributes).
2. The Topology Discovery Agent publishes per-node fabric endpoints in `TenstorrentNodeTopology`; the controller combines these into cluster fabric visibility for ring/fabric-aware placement.
3. A workload controller receives `TenstorrentWorkload` (or claim-based) requests, selects disjoint devices that satisfy topology constraints, and creates exact `ResourceClaims`.
4. The scheduler binds rank pods to nodes; the kubelet plugin validates allocation and injects CDI device access in the container namespace.
5. The Hardware Janitor performs pre-flight scrubbing and continuous health checks; faults trigger tainting/cordoning and reassessment of dependent workloads.
