# Running the Tenstorrent DRA component

This runbook starts the implemented DRA driver, node agents, topology
controller, and Kubernetes resources in the supported QEMU `ttsim` environment.
Run the hardware- and Kubernetes-dependent commands inside the Ubuntu guest.
The guest must provide Docker, kind, kubectl, Helm, Kubernetes v1.34 or newer,
and `tt-kmd`. The driver uses `/dev/tenstorrent`, `/sys/class/tenstorrent`, and
PCI sysfs directly; it does not use `tt-smi`.

## 1. Check the guest prerequisites

Run these commands in the QEMU guest. They confirm the tools and simulated
Tenstorrent paths needed by the driver.

```bash
docker version
kind version
kubectl version --client
helm version
test -e /dev/tenstorrent
test -d /sys/class/tenstorrent
test -d /sys/bus/pci/devices
test -L /sys/bus/pci/devices/<bdf>/iommu_group
```

## 2. Enter the repository

Change to the checkout used by the guest.

```bash
cd /home/varrahan/development/software/tt-device-plugin
```

## 3. Build and check the Go component

These commands compile the driver and run the repository's lightweight checks
before deploying anything to Kubernetes.

```bash
make build
make test
make fmt-check
go vet ./...
```

The combined Make target runs formatting, build, tests, and Helm linting:

```bash
make check
```

## 4. Build the container image

Build the image that contains the DRA driver binary.

```bash
make image-build
```

This creates `tenstorrent-dra:dev`. Use a registry image instead if the cluster
nodes cannot access the local Docker daemon.

## 5. Prepare the validation hardware (optional)

For the disposable synthetic validation environment, create two heterogeneous
node fixtures. Worker A receives one Wormhole and one Blackhole device; worker
B receives one Wormhole device.

```bash
make -C test/vm fake-hardware
```

The target creates the sysfs and PCI fixtures and uses a privileged temporary
Alpine container to create the `/dev/tenstorrent` character devices.

## 6. Run the supported VM validation flow

This is the current automated validation run. It creates a kind cluster named
`tt-dra`, labels both workers for the node DaemonSet, installs the Helm chart,
waits for the workload CRD, and prints ResourceSlices and node topology.

```bash
make -C test/vm vm-validate
```

The validation script mounts the synthetic trees as `/tt-sys` in the kind
workers and passes these paths to the driver. Synthetic character devices do
not implement the `tt-kmd` reset ioctl, so this disposable path explicitly uses
`resetMode=noop` and disables the IOMMU requirement. Never use those overrides
for production hardware.

```text
sysfsRoot=/tt-sys/class/tenstorrent
pciSysfsRoot=/tt-sys/bus/pci/devices
```

If using a locally built image rather than a pullable registry image, create
the cluster first, load the image into every node, and install the chart with
the local image name:

```bash
kind create cluster --name tt-dra --config test/vm/kind/ttsim-dra.yaml
kind load docker-image tenstorrent-dra:dev --name tt-dra
kubectl label node tt-dra-worker tenstorrent.com/enabled=true --overwrite
kubectl label node tt-dra-worker2 tenstorrent.com/enabled=true --overwrite
helm upgrade --install tt-dra deployments/helm/tenstorrent-dra \
  --set image.repository=tenstorrent-dra \
  --set image.tag=dev \
  --set sysfsRoot=/tt-sys/class/tenstorrent \
  --set pciSysfsRoot=/tt-sys/bus/pci/devices \
  --set resetMode=noop \
  --set requireIOMMU=false
```

## 7. Confirm deployment and discovered hardware

Use these commands to confirm that the controller and node agents are running,
and that each host-visible accelerator ASIC was published as a DRA device.

```bash
kubectl get nodes -o wide
kubectl get pods -o wide
kubectl get deviceclasses
kubectl get resourceslices -o yaml
kubectl get tenstorrentnodetopologies -o yaml
kubectl get tenstorrentfabrictopologies -o yaml
kubectl get node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.conditions[?(@.type=="TenstorrentAcceleratorsHealthy")].status}{"\n"}{end}'
```

The synthetic run should show two devices on the first worker, one device on the
second worker, and a valid fabric topology when all expected links are present.

Production uses `resetMode=ioctl` and `requireIOMMU=true`, the chart defaults.
The node agent calls the `tt-kmd` ASIC_RESET and POST_RESET ioctls before and
after each claim. Audit records are stored on the node at:

```bash
sudo tail -n 50 /var/lib/tenstorrent-dra/audit.jsonl
```

## 8. Inspect claims and workload placement

The driver allocates complete host-visible accelerator devices through standard
Kubernetes DRA `ResourceClaim` objects. Replace the image in
[`examples/standard-claim.yaml`](../examples/standard-claim.yaml), apply it, and
inspect allocation and scheduling:

```bash
kubectl apply -f examples/standard-claim.yaml
kubectl get resourceclaims
kubectl get pods -o wide
kubectl describe resourceclaim <claim-name>
kubectl logs <pod-name>
```

For multi-rank, topology-aware placement, apply
[`examples/topology-workload.yaml`](../examples/topology-workload.yaml), then
inspect its assignments and rank Pods:

```bash
kubectl apply -f examples/topology-workload.yaml
kubectl get tenstorrentworkloads -o yaml
kubectl get pods -o wide
```

## 9. Diagnose a failed deployment

The controller reports claims, workload placement, and fabric decisions. The
node DaemonSet reports hardware discovery and CDI generation.

```bash
kubectl logs deployment/tt-dra-controller
kubectl logs daemonset/tt-dra-node
kubectl describe pod <pod-name>
kubectl get events --sort-by=.lastTimestamp
kubectl describe node <node-name>
```

Check the guest's simulated hardware paths if discovery is empty:

```bash
ls -l /dev/tenstorrent
find /sys/class/tenstorrent -maxdepth 3 -type f -o -type l
find /sys/bus/pci/devices -maxdepth 2 -type f -o -type l
```

## 10. Clean up the disposable cluster

Delete the kind cluster after validation. This does not remove the source
checkout or the synthetic fixture directories.

```bash
make -C test/vm kind-clean
```

If the QEMU guest itself must be stopped, use the VM launcher's normal graceful
shutdown procedure from the host; do not terminate the guest while a validation
run is still active.
