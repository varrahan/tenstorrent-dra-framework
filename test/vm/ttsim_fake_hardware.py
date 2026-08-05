#!/usr/bin/env python3
"""Create a small heterogeneous tt-kmd-shaped fixture for VM validation."""

import argparse
import os
from pathlib import Path
import stat


def write(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument(
        "--devices",
        required=True,
        help="comma-separated chip families (wormhole or blackhole)",
    )
    parser.add_argument("--create-device-nodes", action="store_true")
    args = parser.parse_args()
    if args.root.exists():
        for path in sorted(args.root.rglob("*"), reverse=True):
            if path.is_dir() and not path.is_symlink():
                path.rmdir()
            else:
                path.unlink()
    class_root = args.root / "sys" / "class" / "tenstorrent"
    pci_root = args.root / "sys" / "bus" / "pci" / "devices"
    device_root = args.root / "dev" / "tenstorrent"
    chips = args.devices.split(",")
    pci_device_ids = {"wormhole": "0x401e", "blackhole": "0xb140"}
    unsupported = sorted(set(chips) - pci_device_ids.keys())
    if unsupported:
        parser.error(f"unsupported chip families: {', '.join(unsupported)}")
    for index, chip in enumerate(chips):
        name = str(index)
        bdf = f"0000:00:{index + 1:02x}.0"
        data = args.root / "sys" / "devices" / "tt" / name
        pci = pci_root / bdf
        data.mkdir(parents=True, exist_ok=True)
        pci.mkdir(parents=True, exist_ok=True)
        class_entry = class_root / name
        class_entry.parent.mkdir(parents=True, exist_ok=True)
        class_entry.symlink_to(Path("../../devices/tt") / name)
        (data / "device").symlink_to(Path("../../../bus/pci/devices") / bdf)
        write(data / "uevent", f"DEVNAME=/dev/tenstorrent/{name}")
        write(data / "dev", f"226:{index}")
        write(data / "architecture", chip)
        write(data / "health", "Healthy")
        write(data / "device_uuid", f"{args.root.name}-{name}")
        write(data / "kmd_version", "2.10.0")
        write(data / "firmware_version", "19.2.0")
        write(data / "driver_abi_version", "2")
        write(data / "memory_capacity_bytes", "12884901888")
        write(data / "tensix_cores_total", "72")
        write(data / "fabric_id", "fabric-0")
        write(data / "ring_id", "ring-0")
        write(data / "fabric_endpoint", f"{args.root.name}-{name}")
        write(data / "fabric_links" / "link0" / "state", "up")
        write(data / "fabric_links" / "link0" / "remote_endpoint_id", f"{args.root.name}-{(index + 1) % len(chips)}")
        write(pci / "PCI_SLOT_NAME", bdf)
        write(pci / "vendor", "0x1e52")
        write(pci / "device", pci_device_ids[chip])
        write(pci / "numa_node", "0")
        if args.create_device_nodes:
            device_root.mkdir(parents=True, exist_ok=True)
            os.mknod(device_root / name, stat.S_IFCHR | 0o660, os.makedev(226, index))


if __name__ == "__main__":
    main()
