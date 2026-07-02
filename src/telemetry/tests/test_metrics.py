from tt_telemetry.device import DeviceNode
from tt_telemetry.metrics import render_metrics


def test_render_metrics_includes_device_labels() -> None:
    output = render_metrics([DeviceNode(id="0", path="/dev/tenstorrent/0", major=241, minor=0)])

    assert "# TYPE tenstorrent_device_present gauge" in output
    assert 'device="0"' in output
    assert 'path="/dev/tenstorrent/0"' in output
    assert 'major="241"' in output
    assert 'minor="0"' in output
