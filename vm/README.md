# VM Assets

This directory contains VM-level requirements, tests, and configuration that are
independent of the runtime systems under `src/`.

Use this area for QEMU `ttsim` guest setup, host-independent VM validation
configuration, and shared VM prerequisites that apply across the DRA driver,
telemetry exporter, and future node-local components.

Component-specific source, tests, and packaging stay under their owning
`src/<component>/` directory.

## ttsim PCI Device Count

The authoritative baseline is Tenstorrent's
[ttsim QEMU Bridge lesson](https://docs.tenstorrent.com/tt-vscode-toolkit/lessons/ttsim-qemu-bridge/):
use the `stable-11.0-ttsim` QEMU fork, `libttsim_wh.so` from the documented
v1.8.4 setup, TCG with `-cpu max`, a 32 MiB Wormhole BAR4, and
`ttkmd-2.3.0`. Do not use KVM or a newer KMD for this bridge baseline.

Do not use `tt-smi` as a QEMU bridge validation path. Tenstorrent's documented
success criteria are PCI enumeration and compatible KMD binding; current UMD
topology discovery still reaches an unimplemented simulator register. Treat
the QEMU monitor, `lspci`, `tt-kmd` sysfs, and `/dev/tenstorrent/0` as the
bridge sources of truth.

## Optional TT-Metalium Wheel Setup

TT-Metalium is not part of the official QEMU bridge health check. If a separate
experiment needs the Python APIs, use an isolated environment at
`/home/ubuntu/.venvs/tt-metalium`. Do not use `tt-installer` for this VM; that
workflow adds unrelated physical-hardware setup and does not change the
officially documented QEMU bridge limitation.

Optional setup:

```bash
/home/ubuntu/.local/bin/uv venv /home/ubuntu/.venvs/tt-metalium --python /usr/bin/python3
source /home/ubuntu/.venvs/tt-metalium/bin/activate
uv pip install ttnn==0.73.1 pydantic
```

If you create a helper for repeat experiments, source it before running
TT-Metalium Python tools in the VM:

```bash
source /home/ubuntu/tt-metalium-env.sh
```

It should activate the venv, point `TT_METAL_RUNTIME_ROOT` at the wheel's
bundled runtime artifacts, and send generated profiler and TTNN report output
to a writable work directory. Validate imports without opening a device:

```bash
python -c 'import ttnn, ttnn.profiler; print(ttnn.__file__)'
tt-run --help
```

Top-level `tt_lib` imports can require optional model/fused-op dependencies such
as PyTorch. Those dependencies are not VM prerequisites for telemetry exporter
validation or TT-Metalium profiler report collection.

The optional `ttnn==0.73.1` wheel is not Tracy-enabled. It exposes the Python
profiler result API, but device initialization fails if
`TT_METAL_DEVICE_PROFILER=1` is set. Dynamic workload core occupancy therefore
requires a compatible TT-Metalium source build created with:

```bash
git checkout v0.73.1
./build_metal.sh
```

In v0.73.1 Tracy is enabled by default; do not pass `--disable-profiler`. The
equivalent manual CMake setting is `-DENABLE_TRACY=ON`.

Instrumented workloads publish process-local profiler snapshots with
`src/telemetry/integrations/ttnn/metalium_profiler_publisher.py` to a shared
`/var/lib/tt-device-plugin/metalium-profiler` mount. The exporter consumes that
mount with `--metalium-profiler-state-root` and expires stale samples.

Set `TT_METAL_PROFILER_DISABLE_DUMP_TO_FILES=1` so the profiler keeps its
in-process C++ results without writing CSV artifacts. The official bridge
lesson documents that current TTNN topology discovery reaches an unimplemented
ARC tile register. End-to-end profiler samples therefore require compatible
physical hardware; do not use `ttnn.open_device()` as a QEMU bridge check.

Use `check-ttsim-lib.py` from the QEMU host before launching the VM with a new
simulator profile. It loads a `libttsim_*.so` library, probes conventional PCI
BDF device numbers through `libttsim_pci_config_rd32()`, and fails if fewer than
the requested number of simulated cards are host-visible.

```bash
python3 vm/check-ttsim-lib.py /home/varrahan/sim/libttsim_wh.so
python3 vm/check-ttsim-lib.py /home/varrahan/sim/libttsim_wh_x2.so --require-min 1
python3 vm/check-ttsim-lib.py /home/varrahan/sim/libttsim_wh_x8.so --require-min 4
```

Current v1.8.4 host-library observation: `libttsim_wh_x2.so` reports one PCI
function and `libttsim_wh_x8.so` reports four through the config-space API.
Those profile names describe simulated chip configurations, not a documented
QEMU PCI-function count. The official bridge launch uses the single-chip
`libttsim_wh.so`; treat multi-chip QEMU enumeration as unsupported until
Tenstorrent documents it.
