from __future__ import annotations

from collections.abc import Iterable

from .device import DeviceNode


def render_metrics(devices: Iterable[DeviceNode]) -> str:
    lines = [
        "# HELP tenstorrent_device_present Whether a Tenstorrent device node is present.",
        "# TYPE tenstorrent_device_present gauge",
    ]

    for device in devices:
        lines.append(
            'tenstorrent_device_present{'
            f'device="{device.id}",path="{device.path}",major="{device.major}",minor="{device.minor}"'
            "} 1"
        )

    lines.append("")
    return "\n".join(lines)
