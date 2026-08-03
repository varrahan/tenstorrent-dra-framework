# Tenstorrent DRA Model

## Node resources

The node agent treats each host-visible `tt-kmd` character device as one
exclusive whole-card resource. It reads chip/card identity, PCI identity,
memory and compute values, health, and fabric links. Known N150, N300, P100,
and P150 profiles provide fallback capabilities when sysfs omits them;
unknown card pairs remain available through the generic `tenstorrent` class.

ResourceSlices use one node-owned pool and are split at the Kubernetes limit of
128 devices per slice. The driver publishes `deviceID`, `nodeName`,
`chipSeries`, `cardSeries`, `health`, PCI/NUMA, fabric endpoint IDs, and the
profile attributes and capacities needed by selectors. Host paths, permissions,
provenance, and timestamps remain in `tt-dra-driver list` output rather than
scheduler objects.

The installed DeviceClasses are:

- `tenstorrent`
- `tenstorrent-wormhole-n150`
- `tenstorrent-wormhole-n300`
- `tenstorrent-blackhole-p100`
- `tenstorrent-blackhole-p150`

## Standard claims

A Pod can request the generic class and select `chipSeries`, `cardSeries`,
`fabricID`, or `ringID` with CEL. A multi-card request can use DRA's
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
specifies a DeviceClass, card selectors, and a card count. The controller finds
disjoint devices on one node per rank, requires all selected devices to share a
fabric/ring and form a connected graph, then creates exact ResourceClaims and
node-bound Pods. Ranks may share a node when enough cards exist. Card
assignments may differ by rank.

Assignments are pinned to the fabric generation. Before any rank starts, a
changed or unavailable assignment is replanned. Once a rank starts, the
assignment is frozen and the workload reports degradation instead of moving a
running Pod.
