from fastapi import FastAPI
from fastapi.responses import PlainTextResponse

from .device import discover_devices
from .metrics import render_metrics

app = FastAPI(title="Tenstorrent Telemetry")


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/metrics", response_class=PlainTextResponse)
def metrics() -> str:
    devices = discover_devices()
    return render_metrics(devices)
