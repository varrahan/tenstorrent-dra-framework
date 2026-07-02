# DRA Driver

This directory contains the Go implementation of the Tenstorrent Kubernetes
Dynamic Resource Allocation driver.

Initial scope:

- Discover Tenstorrent device nodes exposed by `tt-kmd`.
- Convert local device inventory into Kubernetes DRA resource data.
- Publish `ResourceSlice` objects for Kubernetes v1.34+ clusters.

The first implementation milestone is local device discovery from
`/dev/tenstorrent`. Kubernetes API writes are intentionally kept out of the
initial discovery package so that hardware detection can be tested independently.
