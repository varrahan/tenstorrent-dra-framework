#!/usr/bin/env python3
"""Validate converged claim/CDI snapshots captured by the chaos suite."""

import argparse
import json
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("snapshot", type=Path)
    args = parser.parse_args()

    owners: dict[str, str] = {}
    claim_uids: set[str] = set()
    cdi_uids: set[str] = set()
    nodes = 0

    for node_dir in sorted(path for path in args.snapshot.iterdir() if path.is_dir()):
        state_path = node_dir / "claims.json"
        cdi_path = node_dir / "cdi-files.txt"
        if not state_path.is_file() or not cdi_path.is_file():
            raise SystemExit(f"incomplete safety snapshot for {node_dir.name}")
        nodes += 1
        state = json.loads(state_path.read_text(encoding="utf-8"))
        if state.get("version") != 3:
            raise SystemExit(f"{node_dir.name}: unsupported state version {state.get('version')!r}")
        for uid, claim in state.get("claims", {}).items():
            claim_uids.add(uid)
            phase = claim.get("phase")
            if phase not in {"Preparing", "Prepared", "Releasing", "Recovered"}:
                raise SystemExit(f"{node_dir.name}: claim {uid} has invalid phase {phase!r}")
            devices = claim.get("devices", [])
            if not devices:
                raise SystemExit(f"{node_dir.name}: owned claim {uid} has no devices")
            for device in devices:
                identity = device.get("stableID") or device.get("device")
                if not identity:
                    raise SystemExit(f"{node_dir.name}: claim {uid} has an unnamed device")
                prior = owners.setdefault(identity, uid)
                if prior != uid:
                    raise SystemExit(
                        f"device {identity} is double-owned by claims {prior} and {uid}"
                    )
        for line in cdi_path.read_text(encoding="utf-8").splitlines():
            name = Path(line.strip()).name
            if name.startswith("claim-") and name.endswith(".json"):
                cdi_uids.add(name.removeprefix("claim-").removesuffix(".json"))

    orphaned_cdi = sorted(cdi_uids - claim_uids)
    if orphaned_cdi:
        raise SystemExit(f"CDI files without persisted ownership: {', '.join(orphaned_cdi)}")
    print(
        json.dumps(
            {
                "nodes": nodes,
                "claims": len(claim_uids),
                "ownedDevices": len(owners),
                "cdiFiles": len(cdi_uids),
                "safe": True,
            },
            sort_keys=True,
        )
    )


if __name__ == "__main__":
    main()
