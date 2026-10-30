# Third-party notices

The Laya runtime is from Convai Innovations, pinned to source commit
`fe9a6c35023a7d94102e7feaaeaae8857d9d1b07`, under Apache License 2.0.
The original license is included in `licenses/Laya-LICENSE`.
The `convaiinnovations/laya-multilingual` checkpoint is separately provisioned
from revision `1720e3e3357cfe1e281542e223f8273b0890ca34`; its model card declares
Apache-2.0. Weights are not distributed in this service image.

The adapter wraps pinned SDK interfaces without modifying model weights.
Python dependency distributions retain their own license metadata in the
installed environment. See `uv.lock` for exact dependency sources and versions.
