# Tenstorrent DRA Model

## Node resources

The node agent treats each host-visible `tt-kmd` character device as one
exclusive accelerator ASIC. It reads chip family, PCI identity, observed
memory and compute values, health, and fabric links. PCI device ID identifies
the ASIC as Wormhole or Blackhole when architecture metadata is absent. Board
product names are deliberately excluded: a multi-ASIC product is represented
as multiple devices, while its built-in interconnect is represented by the
topology data.

ResourceSlices use one node-owned pool and are split at the Kubernetes limit of
128 devices per slice. The driver publishes `deviceID`, `nodeName`,
`chipSeries`, `health`, PCI/NUMA, fabric endpoint IDs, and only capacities that
were actually observed. Host paths, permissions, provenance, and timestamps
remain in `tt-dra-driver list` output rather than scheduler objects.

The installed DeviceClasses are:

- `tenstorrent`
- `tenstorrent-wormhole`
- `tenstorrent-blackhole`

## Standard claims

A Pod can request the generic class and select `chipSeries`, `fabricID`, or
`ringID` with CEL. A multi-device request can use DRA's
`matchAttribute` constraint to require a shared fabric or ring. The kubelet
plugin validates the allocation against local inventory and exposes only the
allocated character devices through CDI.

## Topology APIs

Each node publishes `topology.tenstorrent.com/v1alpha1`
`TenstorrentNodeTopology` with endpoint IDs and `remote_endpoint_id` links. The
controller combines fresh node objects into the cluster-scoped
`TenstorrentFabricTopology` singleton. Duplicate endpoints, stale observations,
missing peers, asymmetric links, and cross-ring links make the graph invalid
for topology workloads.

## Distributed workloads

`scheduling.tenstorrent.com/v1alpha1` `TenstorrentWorkload` contains an
explicit target container, a shared Pod template, and ordered ranks. Each rank
specifies a DeviceClass and a device count. The controller finds
disjoint devices on one node per rank, requires all selected devices to share a
fabric/ring and form a connected graph, then creates exact ResourceClaims and
node-bound Pods. Ranks may share a node when enough devices exist. Device
assignments may differ by rank.

Assignments are pinned to the fabric generation. Before any rank starts, a
changed or unavailable assignment is replanned. Once a rank starts, the
assignment is frozen and the workload reports degradation instead of moving a
running Pod.
