# Internal package

This package can't be included by external repositories.
We will maintain methods/functions here that should be shared between our different implementations (e.g. `kubectl-fdb` and the `controller`).
We mark these methods internal to prevent external usage, since we don't give any guarantees for backwards compatibility.
