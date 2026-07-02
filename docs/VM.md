# VM.md — QEMU `ttsim` VM access guide

This document is for agents and developers that need to boot, access, and run work inside the QEMU `ttsim` Ubuntu VM.

The VM is launched with a custom QEMU binary and a simulated Tenstorrent device:

- Guest OS image: Ubuntu 24.04 minimal cloud image
- Runtime device: `-device ttsim,lib=/home/varrahan/sim/libttsim_wh.so,bar4-size=32M`
- Guest tooling already expected in the image: Docker, kind, `kubectl`, and
  `tt-kmd`
- Console mode: `-nographic`
- Recommended access path: SSH from host to guest through QEMU user-networking port forwarding

> Important: the original command uses QEMU user-mode networking without a `hostfwd` rule. In that form, the guest can usually make outbound network connections, but agents on the host cannot SSH into the guest through `127.0.0.1:<port>`. Use the recommended command below to expose guest SSH on host `127.0.0.1:2222`.

---

## 1. Host prerequisites

Run these checks from the QEMU build directory on the QEMU host before starting
the VM:

```bash
test -x ./build/qemu-system-x86_64
test -r /usr/share/ovmf/OVMF.fd
test -r /home/varrahan/images/ubuntu-24.04-minimal-cloudimg-amd64.img
test -r /home/varrahan/images/seed.iso
test -r /home/varrahan/sim/libttsim_wh.so
test -r /dev/kvm && test -w /dev/kvm
```

Check whether the default SSH-forward port is free:

```bash
ss -ltnp | grep ':2222 ' || true
```

If port `2222` is already in use, pick another host port such as `2223` and use it consistently in both the QEMU command and SSH commands.

---

## 2. Recommended launch command with SSH access

Start the VM from the QEMU build directory where
`./build/qemu-system-x86_64` exists:

```bash
cd ~/emulators/ttsim-qemu

./build/qemu-system-x86_64 \
  -m 8G \
  -smp 4 \
  -enable-kvm \
  -nographic \
  -bios /usr/share/ovmf/OVMF.fd \
  -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22 \
  -device virtio-net-pci,netdev=net0 \
  -drive file=/home/varrahan/images/ubuntu-24.04-minimal-cloudimg-amd64.img,format=qcow2,if=virtio \
  -cdrom /home/varrahan/images/seed.iso \
  -device ttsim,lib=/home/varrahan/sim/libttsim_wh.so,bar4-size=32M
```

This maps:

```text
host 127.0.0.1:2222  ->  guest 127.0.0.1:22
```

Keep the bind address as `127.0.0.1` unless there is a specific need to expose the VM to other machines. Binding to all interfaces can make the VM reachable by other hosts on the network.

---

## 3. Detached launch for agents

For long-running agent work, run QEMU inside `tmux` so the VM survives terminal disconnects:

```bash
tmux new -s ttsim-vm
```

Then paste the recommended launch command above.

Useful `tmux` controls:

```text
Detach from session:       Ctrl-b d
Reattach to session:       tmux attach -t ttsim-vm
List sessions:             tmux ls
Kill session if needed:    tmux kill-session -t ttsim-vm
```

Graceful shutdown is preferred over killing the QEMU process.

---

## 4. Discover the VM SSH user

The username and SSH keys are controlled by the cloud-init seed ISO at:

```text
/home/varrahan/images/seed.iso
```

Inspect it from the host when the VM is not relying on a mounted seed directory.
Prefer `isoinfo` when it is available, because it does not require sudo:

```bash
isoinfo -R -i /home/varrahan/images/seed.iso -f
isoinfo -R -i /home/varrahan/images/seed.iso -x /user-data
isoinfo -R -i /home/varrahan/images/seed.iso -x /meta-data
```

If `isoinfo` is not available, mount the ISO temporarily:

```bash
mkdir -p /tmp/ttsim-seed
sudo mount -o loop /home/varrahan/images/seed.iso /tmp/ttsim-seed
sed -n '1,240p' /tmp/ttsim-seed/user-data 2>/dev/null || true
sed -n '1,120p' /tmp/ttsim-seed/meta-data 2>/dev/null || true
sudo umount /tmp/ttsim-seed
```

Look for fields such as:

```yaml
users:
  - name: <vm_user>
ssh_authorized_keys:
  - ssh-ed25519 ...
```

The current seed image enables SSH password authentication:

```yaml
password: ubuntu
ssh_pwauth: True
```

It does not define a custom user, so use the Ubuntu cloud image default user
`ubuntu` unless the seed ISO is changed. Prefer key-based SSH when the seed
configures keys; otherwise use the seed-provided password.

---

## 5. SSH into the VM

After the VM reaches the login prompt or cloud-init finishes, connect from the host:

```bash
ssh -p 2222 ubuntu@127.0.0.1
```

Recommended SSH config entry on the host:

```sshconfig
Host ttsim-vm
  HostName 127.0.0.1
  Port 2222
  User ubuntu
  StrictHostKeyChecking accept-new
```

Then agents can run:

```bash
ssh ttsim-vm 'hostname && uptime'
```

Copy files into the VM:

```bash
scp -P 2222 ./local-file ubuntu@127.0.0.1:/tmp/
```

Run a command inside the VM:

```bash
ssh -p 2222 ubuntu@127.0.0.1 'bash -lc "docker version && kind version"'
```

If the host key changes after rebuilding the image, clear the old key entry:

```bash
ssh-keygen -R '[127.0.0.1]:2222'
```

---

## 6. Access from containerized agents

If an agent itself runs inside a Docker container on the QEMU host, `127.0.0.1` usually refers to the agent container, not the host. Prefer host networking for the agent container:

```bash
docker run --rm -it --network host <agent-image> bash
```

Then inside the agent container:

```bash
ssh -p 2222 ubuntu@127.0.0.1
```

For remote agents that cannot run on the QEMU host, create an SSH tunnel to the host first:

```bash
ssh -N -L 2222:127.0.0.1:2222 <host_user>@<qemu_host>
```

Then, from the remote agent machine:

```bash
ssh -p 2222 ubuntu@127.0.0.1
```

---

## 7. VM console access

Because QEMU is launched with `-nographic`, the VM console appears in the terminal running QEMU.

Useful console controls:

```text
QEMU help:                 Ctrl-a h
Switch console/monitor:    Ctrl-a c
Terminate QEMU:            Ctrl-a x
```

Prefer these shutdown methods in order:

```bash
# From inside the guest:
sudo shutdown -h now

# From the host through SSH:
ssh -p 2222 ubuntu@127.0.0.1 'sudo shutdown -h now'
```

Use `Ctrl-a x` only when graceful shutdown is not possible.

---

## 8. Initial guest verification

After SSH works, verify the guest environment:

```bash
cat /etc/os-release
uname -a
ip addr
systemctl is-active ssh || systemctl status ssh --no-pager
```

Verify Docker:

```bash
docker version || sudo docker version
docker ps || sudo docker ps
sudo systemctl status docker --no-pager
```

Verify kind and `kubectl`:

```bash
kind version
kubectl version --client 2>/dev/null || true
```

If this repository is available inside the VM, the checked-in validation wrapper
runs the `tt-kmd`, kind, DRA API, and pod device-visibility checks:

```bash
make -C test/vm vm-validate
```

Use the inline commands below when debugging a specific validation step.

Create a DRA-capable smoke-test kind cluster when needed. Kubernetes v1.34+ is
required for this project, so pin the kind node image instead of relying on the
default image. This check assumes `tt-kmd` is loaded and
`/dev/tenstorrent/<device>` exists; if it does not, complete the `tt-kmd`
verification section first.

```bash
KINDEST_NODE_IMAGE="${KINDEST_NODE_IMAGE:-kindest/node:v1.34.0}"

TT_DEVICE_PATH="${TT_DEVICE_PATH:-/dev/tenstorrent}"
if [ ! -e "$TT_DEVICE_PATH" ]; then
  TT_DEVICE_PATH="$(find /dev -maxdepth 1 -name 'tenstorrent*' -print | sort | head -n 1)"
fi
test -n "$TT_DEVICE_PATH"
test -e "$TT_DEVICE_PATH"
find "$TT_DEVICE_PATH" -maxdepth 1 -type c -print -quit | grep -q .

cat >/tmp/ttsim-kind.yaml <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: ${KINDEST_NODE_IMAGE}
  extraMounts:
  - hostPath: ${TT_DEVICE_PATH}
    containerPath: ${TT_DEVICE_PATH}
    propagation: HostToContainer
EOF

kind create cluster --name agent-smoke --config /tmp/ttsim-kind.yaml --wait 120s
kubectl cluster-info --context kind-agent-smoke
kubectl --context kind-agent-smoke version
kubectl --context kind-agent-smoke api-resources --api-group=resource.k8s.io
kubectl --context kind-agent-smoke api-resources --api-group=resource.k8s.io | grep --color=never -E '^(deviceclasses|resourceclaims|resourceslices)[[:space:]]'

docker exec agent-smoke-control-plane test -e "$TT_DEVICE_PATH"
docker exec agent-smoke-control-plane find "$TT_DEVICE_PATH" -maxdepth 1 -type c -ls
```

Verify that a pod can see the mounted device path through a `hostPath` mount:

```bash
cat >/tmp/ttsim-device-check.yaml <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ttsim-device-check
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: check
        image: busybox:1.36
        command: ["sh", "-c", "find ${TT_DEVICE_PATH} -maxdepth 1 -type c -print -quit | grep -q ."]
        securityContext:
          privileged: true
        volumeMounts:
        - name: ttsim-device
          mountPath: ${TT_DEVICE_PATH}
      volumes:
      - name: ttsim-device
        hostPath:
          path: ${TT_DEVICE_PATH}
          type: Directory
EOF

kubectl --context kind-agent-smoke apply -f /tmp/ttsim-device-check.yaml
kubectl --context kind-agent-smoke wait --for=condition=complete job/ttsim-device-check --timeout=120s
kubectl --context kind-agent-smoke logs job/ttsim-device-check
kubectl --context kind-agent-smoke delete job ttsim-device-check --ignore-not-found
kind delete cluster --name agent-smoke
```

If `tt-kmd` exposes multiple device paths, repeat the `extraMounts`, `hostPath`,
and `volumeMounts` entries for each path needed by the workflow under test. If
the guest image uses a non-`/dev/tenstorrent*` device name, set
`TT_DEVICE_PATH` explicitly before creating the cluster.

---

## 9. Verify `ttsim` / `tt-kmd`

Inside the VM, check for the simulated PCI device and driver state:

```bash
lspci -nn | grep --color=never -i -E 'tenstorrent|device' || lspci -nn
lspci -nnk -d 1e52:
lsmod | grep --color=never -i tenstorrent || true
sudo dmesg | grep --color=never -i -E 'tenstorrent|ttsim|tt-kmd' | tail -100 || true
find /dev/tenstorrent -maxdepth 1 -mindepth 0 -ls 2>/dev/null || true
```

In the current VM image, the `tt-kmd` source tree and built kernel module are at
`/home/ubuntu/tt-kmd`, but the module is not installed under `/lib/modules`.
Load it directly if `/dev/tenstorrent/0` is missing:

```bash
modinfo /home/ubuntu/tt-kmd/tenstorrent.ko | sed -n '1,80p'
sudo insmod /home/ubuntu/tt-kmd/tenstorrent.ko || true
lsmod | grep --color=never -i tenstorrent || true
lspci -nnk -d 1e52:
find /dev/tenstorrent -maxdepth 1 -type c -ls
```

Use `sudo modprobe tenstorrent` only after the module is installed into the
running kernel's module tree. The exact module and device-node names depend on
the installed `tt-kmd` version and guest image configuration, so use `sudo
dmesg`, `lspci`, `lsmod`, and `/dev` discovery as the source of truth.

---

## 10. Exposing guest or kind services

For a temporary service inside the VM, prefer SSH forwarding instead of adding more QEMU ports:

```bash
ssh -p 2222 -N -L 8080:127.0.0.1:8080 ubuntu@127.0.0.1
```

For a service running inside kind, port-forward from Kubernetes to the VM first:

```bash
kubectl port-forward --address 127.0.0.1 svc/<service-name> 8080:<service-port>
```

Then use the SSH tunnel above to reach it from the host.

If a stable host-to-guest port is required, add another `hostfwd` entry to the `-netdev` argument before booting the VM, for example:

```bash
-netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22,hostfwd=tcp:127.0.0.1:8080-:8080
```

---

## 11. Multiple agents or multiple VM instances

Do not boot the same mutable qcow2 image in more than one QEMU process at the same time. That can corrupt the disk image.

For multiple concurrent VM instances, use qcow2 overlays and unique SSH ports:

```bash
BASE=/home/varrahan/images/ubuntu-24.04-minimal-cloudimg-amd64.img
qemu-img create -f qcow2 -F qcow2 -b "$BASE" /home/varrahan/images/agent-1-overlay.qcow2
qemu-img create -f qcow2 -F qcow2 -b "$BASE" /home/varrahan/images/agent-2-overlay.qcow2
```

Then launch each VM with a different disk and host port:

```text
agent 1: -drive file=/home/varrahan/images/agent-1-overlay.qcow2,... -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22
agent 2: -drive file=/home/varrahan/images/agent-2-overlay.qcow2,... -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2223-:22
```

For most workflows, it is simpler for several agents to share one running VM through separate SSH sessions.

---

## 12. Troubleshooting

### SSH connection refused

Check these first:

```bash
ss -ltnp | grep ':2222 ' || true
```

- If nothing is listening on `2222`, confirm the QEMU command includes `hostfwd=tcp:127.0.0.1:2222-:22`.
- If QEMU is listening but SSH refuses, the guest may still be booting or `sshd` may not be running.
- Use the QEMU console to inspect boot and cloud-init progress.

### SSH hangs

Try:

```bash
ssh -vvv -p 2222 ubuntu@127.0.0.1
```

Then check the VM console for boot, cloud-init, or network issues.

### KVM permission denied

Check host permissions:

```bash
ls -l /dev/kvm
id
```

Run QEMU with a user that can access `/dev/kvm`, or use the host’s standard process for adding the user to the `kvm` group.

### QEMU says the `ttsim` device or library cannot load

Check the library path and dependencies:

```bash
ls -l /home/varrahan/sim/libttsim_wh.so
ldd /home/varrahan/sim/libttsim_wh.so
```

Also confirm the custom QEMU binary was built with the `ttsim` device support expected by this command.

### Docker permission denied inside guest

Try with `sudo` first:

```bash
sudo docker ps
```

If that works, the VM user may not be in the `docker` group, or the session may need to be restarted after group membership changes.

### kind cluster fails to start

Check Docker health and available space:

```bash
sudo systemctl status docker --no-pager
docker info || sudo docker info
df -h
docker system df || sudo docker system df
```

Clean up old kind clusters if needed:

```bash
kind get clusters
kind delete cluster --name <cluster-name>
```

---

## 13. Agent handoff template

Fill this in when handing the VM to another agent:

```text
QEMU host:                  <hostname or ssh target for the physical host>
VM SSH host from QEMU host:  127.0.0.1
VM SSH port:                2222
VM SSH user:                ubuntu
VM auth method:             seed ISO password unless SSH keys are configured
QEMU tmux session:           ttsim-vm
Disk image:                 /home/varrahan/images/ubuntu-24.04-minimal-cloudimg-amd64.img
Cloud-init seed:             /home/varrahan/images/seed.iso
ttsim library:               /home/varrahan/sim/libttsim_wh.so
Expected guest tools:        docker, kind, kubectl, tt-kmd
Shutdown command:            ssh -p 2222 ubuntu@127.0.0.1 'sudo shutdown -h now'
```

Agents should begin by running:

```bash
ssh -p 2222 ubuntu@127.0.0.1 'bash -lc "hostname; uptime; docker version || sudo docker version; kind version; lspci -nn | head"'
```

---

## 14. Reference

QEMU user-mode networking is NAT-style and not directly reachable from outside unless host forwarding is configured. The recommended SSH forwarding pattern is:

```bash
-netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22
```
