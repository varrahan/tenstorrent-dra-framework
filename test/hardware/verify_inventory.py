#!/usr/bin/env python3
"""Fail-closed verification of a physical tt-dra-driver inventory snapshot."""

import argparse
import json
from pathlib import Path


def version_prefix(value: str) -> str:
    return value.removesuffix(".x")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("matrix", type=Path)
    parser.add_argument("entry")
    parser.add_argument("inventory", type=Path)
    args = parser.parse_args()

    matrix = json.loads(args.matrix.read_text(encoding="utf-8"))
    entries = {entry["id"]: entry for entry in matrix["entries"]}
    if args.entry not in entries:
        raise SystemExit(f"unknown matrix entry: {args.entry}")
    expected = entries[args.entry]
    inventory = json.loads(args.inventory.read_text(encoding="utf-8"))
    devices = inventory.get("devices", [])
    if not devices:
        raise SystemExit("inventory contains no devices")

    stable_ids: set[str] = set()
    counts = {"wormhole": 0, "blackhole": 0}
    for item in devices:
        identity = item.get("stableID")
        if not identity or identity in stable_ids:
            raise SystemExit(f"missing or duplicate stable identity: {identity!r}")
        stable_ids.add(identity)
        if item.get("health") != "Healthy" or item.get("eligible") is not True:
            raise SystemExit(f"{identity}: device is not explicitly healthy and eligible")
        if item.get("characterDevicePresent") is not True:
            raise SystemExit(f"{identity}: character device is absent")
        chip = item.get("chipSeries")
        if chip not in counts:
            raise SystemExit(f"{identity}: unsupported chip series {chip!r}")
        counts[chip] += 1
        if not item.get("kmdVersion", "").startswith(version_prefix(expected["kmd"]) + "."):
            raise SystemExit(f"{identity}: KMD {item.get('kmdVersion')!r} does not match {expected['kmd']}")
        if not item.get("firmwareVersion", "").startswith("19.2."):
            raise SystemExit(f"{identity}: unsupported firmware {item.get('firmwareVersion')!r}")
        if item.get("driverABIVersion") != 2:
            raise SystemExit(f"{identity}: unsupported driver ABI {item.get('driverABIVersion')!r}")
        if not item.get("kernelVersion", "").startswith(version_prefix(expected["kernel"]) + "."):
            raise SystemExit(f"{identity}: kernel {item.get('kernelVersion')!r} does not match {expected['kernel']}")
        pci = item.get("pci", {})
        if pci.get("iommuGroup", -1) < 0 or pci.get("iommuGroupSize") != 1:
            raise SystemExit(f"{identity}: device does not have a dedicated IOMMU group")
        for link in item.get("fabric", {}).get("links", []):
            if link.get("state", "").lower() != "up":
                raise SystemExit(f"{identity}: fabric link {link.get('name')} is not up")

    if counts != expected["chipCounts"]:
        raise SystemExit(f"chip counts {counts!r} do not match matrix entry {expected['chipCounts']!r}")
    print(json.dumps({"entry": args.entry, "devices": len(devices), "safe": True}, sort_keys=True))


if __name__ == "__main__":
    main()
