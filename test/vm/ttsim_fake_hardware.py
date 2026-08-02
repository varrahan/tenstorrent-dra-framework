#!/usr/bin/env python3
"""Generate fake Tenstorrent sysfs trees and optional workload state.

The intent is to support end-to-end validation of hwmon and live workload
telemetry ingestion without physical cards. The generated tree is synthetic;
it does not replace the tt-kmd character devices required by kind validation.
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import random
import shutil
import stat
import time


DEFAULT_SYSFS_ROOT = Path("/tmp/fake_sysfs")
DEFAULT_STATE_ROOT = Path("/tmp/fake_metalium_state")
DEFAULT_INTERVAL = 2.0
DEFAULT_DEVICES = 1


def _write_value(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(f"{value}\n", encoding="utf-8")


def _safe_symlink(path: Path, target: Path) -> None:
    if path.is_symlink() or path.exists():
        path.unlink()
    path.symlink_to(target)


def _safe_unlink(path: Path) -> None:
    if path.is_symlink() or (path.exists() and not path.is_dir()):
        path.unlink(missing_ok=True)
    elif path.is_dir():
        shutil.rmtree(path)


def setup_mock_sysfs(
    root: Path,
    device_count: int,
    fixture: str = "normal",
    device_root: Path | None = None,
    device_major: int = 241,
    create_device_nodes: bool = False,
) -> None:
    if root.exists():
        shutil.rmtree(root)
    class_root = root / "class" / "tenstorrent"
    class_root.mkdir(parents=True, exist_ok=True)
    (root / "devices" / "pci0000:00").mkdir(parents=True, exist_ok=True)

    for i in range(device_count):
        if fixture in ("missing", "hotplug") and i == device_count - 1:
            if create_device_nodes and device_root is not None:
                _safe_unlink(device_root / str(i))
            continue
        index = str(i)
        pci_addr = f"0000:00:{i + 1:02x}.0"
        pci_dir = root / "devices" / "pci0000:00" / pci_addr
        device_dir = class_root / index

        _safe_unlink(device_dir)
        device_dir.mkdir(parents=True, exist_ok=True)

        # PCI identity and transport metadata.
        _write_value(pci_dir / "PCI_SLOT_NAME", pci_addr)
        _write_value(pci_dir / "vendor", "0xdead" if fixture == "malformed" else "0x1e52")
        _write_value(pci_dir / "device", "0x401e")
        _write_value(pci_dir / "class", "0x120000")
        _write_value(pci_dir / "subsystem_vendor", "0x1e36")
        _write_value(pci_dir / "subsystem_device", "0x0001")
        _write_value(pci_dir / "numa_node", "0")
        _write_value(pci_dir / "revision", "0x01")
        _write_value(pci_dir / "current_link_speed", "16.0 GT/s PCIe")
        _write_value(pci_dir / "current_link_width", "8")
        _write_value(pci_dir / "max_link_speed", "32.0 GT/s PCIe")
        _write_value(pci_dir / "max_link_width", "16")
        _write_value(
            pci_dir / "resource",
            "\n".join(
                [
                    "0000000000000000 0000000000000000 0000000000000000",
                    "0000000000000000 0000000000000000 0000000000000000",
                    "0000000000000000 000000000003fffff 0000000000000000",
                    "0000000000000000 000000000000ffff 0000000000000000",
                    "0000000000000000 00000000001fffff 0000000000000000",
                ]
            ),
        )
        _write_value(
            pci_dir / "uevent", f"DRIVER=tenstorrent\nPCI_SLOT_NAME={pci_addr}\n"
        )
        _safe_symlink(device_dir / "device", pci_dir)

        # Tenstorrent device attributes expected by the telemetry workflow.
        device_path = (device_root / index) if device_root is not None else Path("/dev/tenstorrent") / index
        _write_value(device_dir / "uevent", f"DEVNAME={device_path}\n")
        _write_value(device_dir / "dev", "not-a-device" if fixture == "malformed" else f"{226 + i}:{i}")
        _write_value(device_dir / "architecture", "unknown" if fixture == "malformed" else "wormhole")
        _write_value(device_dir / "arch", "wormhole")
        _write_value(device_dir / "board_type", "mystery" if fixture == "malformed" else "n300")
        _write_value(device_dir / "health", "Unhealthy" if fixture == "unhealthy" else "Healthy")
        _write_value(device_dir / "tt_fw_bundle_ver", "v2.0.0")
        _write_value(device_dir / "tt_aiclk", 1000000000)
        _write_value(device_dir / "tt_axiclk", 800000000)
        _write_value(device_dir / "tt_arcclk", 500000000)
        _write_value(device_dir / "memory_capacity_bytes", 12 * 1024 * 1024 * 1024)
        _write_value(device_dir / "memory_used_bytes", 1024 * 1024 * 1024)
        _write_value(device_dir / "memory_available_bytes", 11 * 1024 * 1024 * 1024)
        _write_value(device_dir / "tensix_cores_total", 72)
        _write_value(device_dir / "tensix_cores_used", 0)
        _write_value(device_dir / "tensix_topology", "2dMesh")
        _write_value(device_dir / "fault_code", 0)
        # Fabric identity lets the synthetic tree exercise topology-aware
        # placement as well as local inventory and health handling.
        _write_value(device_dir / "fabric_id", "fabric-0")
        _write_value(device_dir / "ring_id", "ring-0")
        _write_value(device_dir / "fabric_endpoint", f"ttsim-{index}")

        if create_device_nodes and not (fixture == "missing" and i == device_count - 1):
            if device_root is None:
                raise SystemExit("--create-device-nodes requires --device-root")
            device_root.mkdir(parents=True, exist_ok=True)
            node_path = device_root / index
            _safe_unlink(node_path)
            try:
                os.mknod(node_path, stat.S_IFCHR | 0o660, os.makedev(device_major, i))
            except PermissionError as exc:
                raise SystemExit(
                    f"cannot create {node_path}: run as root or choose a writable device root"
                ) from exc

        # hwmon at the supported roots.
        _write_value(device_dir / "hwmon" / f"hwmon{i}" / "name", "tenstorrent")
        _write_value(device_dir / "hwmon" / f"hwmon{i}" / "temp1_label", "asic")
        _write_value(device_dir / "hwmon" / f"hwmon{i}" / "temp1_crit", 85000)
        _write_value(device_dir / "hwmon" / f"hwmon{i}" / "power1_label", "board")
        _write_value(device_dir / "hwmon" / f"hwmon{i}" / "power1_cap", 300000000)
        _write_value(
            device_dir / "hwmon" / f"hwmon{i}" / "temp1_input",
            random.randint(40000, 65000),
        )
        _write_value(
            device_dir / "hwmon" / f"hwmon{i}" / "power1_input",
            random.randint(100000000, 150000000),
        )

        _write_value(device_dir / "fabric_links" / "link0" / "state", "down" if fixture == "link-down" else "up")
        _write_value(device_dir / "fabric_links" / "link0" / "speed_gbps", 16)
        _write_value(
            device_dir / "fabric_links" / "link0" / "remote_bdf",
            f"ttsim-{(i + 1) % device_count}",
        )


def simulate_hardware_loop(root: Path, device_count: int) -> None:
    for i in range(device_count):
        pci_addr = f"0000:00:{i + 1:02x}.0"
        device_dir = root / "class" / "tenstorrent" / str(i)
        if not device_dir.exists():
            continue
        _write_value(
            device_dir / f"hwmon/hwmon{i}/temp1_input",
            random.randint(40000, 65000),
        )
        _write_value(
            device_dir / f"hwmon/hwmon{i}/power1_input",
            random.randint(100000000, 150000000),
        )
        _write_value(device_dir / "tt_aiclk", random.randint(900, 1200) * 1_000_000)
        _write_value(
            device_dir / "memory_used_bytes", random.randint(1_000_000_000, 8_000_000_000)
        )
        _write_value(device_dir / "tensix_cores_used", random.randint(0, 72))
        _write_value(
            root / "devices" / "pci0000:00" / pci_addr / "current_link_speed",
            random.choice(("16.0 GT/s PCIe", "32.0 GT/s PCIe")),
        )
        _write_value(
            root / "devices" / "pci0000:00" / pci_addr / "current_link_width",
            random.choice((8, 16)),
        )


def simulate_system(
    root: Path,
    device_count: int,
    interval: float,
    iterations: int,
    state_root: Path | None = None,
    inject_workloads: bool = False,
    fixture: str = "normal",
    device_root: Path | None = None,
    device_major: int = 241,
    create_device_nodes: bool = False,
) -> None:
    if iterations <= 0:
        iteration = 0
        while True:
            if fixture == "hotplug" and iteration == 1:
                setup_mock_sysfs(root, device_count, "normal", device_root, device_major, create_device_nodes)
            simulate_hardware_loop(root, device_count)
            if inject_workloads and state_root is not None:
                simulate_workloads(state_root, device_count, interval, 1)
            iteration += 1
            time.sleep(interval)
        return

    for index in range(iterations):
        if fixture == "hotplug" and index == 1:
            setup_mock_sysfs(root, device_count, "normal", device_root, device_major, create_device_nodes)
        simulate_hardware_loop(root, device_count)
        if inject_workloads and state_root is not None:
            simulate_workloads(state_root, device_count, interval, 1)
        if index + 1 < iterations:
            time.sleep(interval)


def write_workload_sample(
    state_root: Path,
    device_id: str,
    pod_uid: str,
    program_count: int,
    total_cores: int,
    cores_used: int,
) -> None:
    payload = "\n".join(
        [
            "schema_version=2",
            "workload_id=synthetic",
            "active=1",
            f"programs_observed={program_count}",
            f"tensix_cores_used={cores_used}",
            f"tensix_cores_total={total_cores}",
            f"sample_timestamp_seconds={int(time.time())}",
            "pod_namespace=ttsim",
            "pod_name=synthetic-pod",
            "container_name=synthetic-container",
            "",
        ]
    )
    _write_value(
        state_root / "v2" / "workloads" / pod_uid / device_id / "snapshot.state",
        payload,
    )


def simulate_workloads(
    state_root: Path,
    device_count: int,
    interval: float,
    total_iterations: int,
) -> None:
    iterations = 0
    while total_iterations == 0 or iterations < total_iterations:
        for i in range(device_count):
            payload_cores = random.randint(0, 72)
            write_workload_sample(
                state_root,
                str(i),
                f"pod-{i}",
                random.randint(1, 8),
                72,
                payload_cores,
            )
        iterations += 1
        if total_iterations and iterations >= total_iterations:
            break
        time.sleep(interval)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate fake Tenstorrent sysfs hardware state."
    )
    parser.add_argument(
        "--sysfs-root",
        type=Path,
        default=DEFAULT_SYSFS_ROOT,
        help=f"Root where fake sysfs is rendered (default: {DEFAULT_SYSFS_ROOT})",
    )
    parser.add_argument(
        "--state-root",
        type=Path,
        default=DEFAULT_STATE_ROOT,
        help=f"Root for fake profiler state writes (default: {DEFAULT_STATE_ROOT})",
    )
    parser.add_argument("--device-count", type=int, default=DEFAULT_DEVICES)
    parser.add_argument(
        "--device-root",
        type=Path,
        help="Root for simulated character devices; used with --create-device-nodes.",
    )
    parser.add_argument(
        "--device-major",
        type=int,
        default=241,
        help="Major number for simulated character devices (default: 241).",
    )
    parser.add_argument(
        "--create-device-nodes",
        action="store_true",
        help="Create Linux character-device stand-ins; requires root/CAP_MKNOD.",
    )
    parser.add_argument(
        "--fixture",
        choices=("normal", "missing", "malformed", "unhealthy", "hotplug", "link-down"),
        default="normal",
        help="fixture variant for inventory failure testing",
    )
    parser.add_argument("--interval", type=float, default=DEFAULT_INTERVAL)
    parser.add_argument(
        "--iterations",
        type=int,
        default=0,
        help="Number of update iterations; 0 means infinite.",
    )
    parser.add_argument(
        "--simulate-workloads",
        action="store_true",
        help="Write synthetic TT-Metalium v2 workload state.",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    if args.device_count < 1:
        raise SystemExit("device-count must be >= 1")
    if args.interval <= 0:
        raise SystemExit("interval must be > 0")
    if args.device_major < 0:
        raise SystemExit("device-major must be >= 0")
    if args.create_device_nodes and args.device_root is None:
        raise SystemExit("--create-device-nodes requires --device-root")

    setup_mock_sysfs(
        args.sysfs_root,
        args.device_count,
        args.fixture,
        args.device_root,
        args.device_major,
        args.create_device_nodes,
    )
    print(f"[ttsim] initialized fake sysfs at {args.sysfs_root}")

    if args.simulate_workloads:
        (args.state_root / "v2" / "workloads").mkdir(parents=True, exist_ok=True)
    simulate_system(
        root=args.sysfs_root,
        device_count=args.device_count,
        interval=args.interval,
        iterations=args.iterations,
        state_root=args.state_root if args.simulate_workloads else None,
        inject_workloads=args.simulate_workloads,
        fixture=args.fixture,
        device_root=args.device_root,
        device_major=args.device_major,
        create_device_nodes=args.create_device_nodes,
    )


if __name__ == "__main__":
    main()
