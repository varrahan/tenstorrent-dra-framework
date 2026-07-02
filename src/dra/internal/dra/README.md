# DRA Resource Model

This package contains the DRA-facing resource model.

The current code maps discovered Tenstorrent device nodes into a lightweight
internal model. The next implementation step is converting that model into
Kubernetes `resource.k8s.io/v1` objects, starting with `ResourceSlice`.
