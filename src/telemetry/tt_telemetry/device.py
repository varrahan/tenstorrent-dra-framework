from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import stat


@dataclass(frozen=True)
class DeviceNode:
    id: str
    path: str
    major: int
    minor: int


def discover_devices(root: str = "/dev/tenstorrent") -> list[DeviceNode]:
    root_path = Path(root)
    if not root_path.exists():
        return []

    if root_path.is_char_device():
        return [_device_from_path(root_path)]

    if not root_path.is_dir():
        return []

    devices: list[DeviceNode] = []
    for path in sorted(root_path.iterdir(), key=lambda candidate: candidate.name):
        if path.is_char_device():
            devices.append(_device_from_path(path))
    return devices


def _device_from_path(path: Path) -> DeviceNode:
    info = path.stat()
    return DeviceNode(
        id=path.name,
        path=str(path),
        major=stat.major(info.st_rdev),
        minor=stat.minor(info.st_rdev),
    )
