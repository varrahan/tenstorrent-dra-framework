#!/usr/bin/env python3
"""Validate host-visible PCI devices exposed by a libttsim shared library."""

from __future__ import annotations

import argparse
import ctypes
import sys
from pathlib import Path


PCI_VENDOR_DEVICE_OFFSET = 0x0
PCI_ABSENT_VALUE = 0xFFFFFFFF


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Load a libttsim shared library and count the host-visible PCI "
            "devices it exposes through libttsim_pci_config_rd32()."
        )
    )
    parser.add_argument("library", type=Path, help="path to libttsim_*.so")
    parser.add_argument(
        "--max-devices",
        type=int,
        default=32,
        help="maximum PCI device numbers to probe on bus 0 function 0",
    )
    parser.add_argument(
        "--require-min",
        type=int,
        default=1,
        help="fail unless at least this many devices are discovered",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.max_devices < 1:
        print("--max-devices must be at least 1", file=sys.stderr)
        return 2
    if args.require_min < 0:
        print("--require-min must be non-negative", file=sys.stderr)
        return 2
    if not args.library.is_file():
        print(f"library not found: {args.library}", file=sys.stderr)
        return 2

    lib = ctypes.CDLL(str(args.library))
    init = lib.libttsim_init
    exit_ = lib.libttsim_exit
    config_rd32 = lib.libttsim_pci_config_rd32
    config_rd32.argtypes = [ctypes.c_uint32, ctypes.c_uint32]
    config_rd32.restype = ctypes.c_uint32

    found = []
    initialized = False
    try:
        init()
        initialized = True
        for device in range(args.max_devices):
            # libttsim expects a conventional BDF value. Bus and function must
            # remain zero; the device number selects the host-visible chip.
            bdf = device << 3
            vendor_device = config_rd32(bdf, PCI_VENDOR_DEVICE_OFFSET)
            if vendor_device == PCI_ABSENT_VALUE:
                print(f"device {device:02d}: absent")
                continue

            vendor_id = vendor_device & 0xFFFF
            device_id = (vendor_device >> 16) & 0xFFFF
            found.append(device)
            print(
                f"device {device:02d}: present "
                f"bdf=0x{bdf:02x} vendor=0x{vendor_id:04x} "
                f"device=0x{device_id:04x}"
            )
    finally:
        if initialized:
            exit_()

    print(f"devices_found={len(found)}")
    if len(found) < args.require_min:
        print(
            f"expected at least {args.require_min} device(s), "
            f"found {len(found)}",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
