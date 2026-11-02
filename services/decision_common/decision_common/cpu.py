"""Select Transformers' reference PyTorch kernels for CPU inference."""
import importlib
import sys


def prepare_cpu_kernels():
    # Transformers selects optional FLA at module import time. FLA's Triton
    # kernels require a GPU even if installed on a CPU-only machine. Import the
    # pinned Qwen implementation while FLA is unavailable so its documented
    # PyTorch fallback is selected for this worker's lifetime.
    previous = sys.modules.get('fla', ...)
    sys.modules['fla'] = None
    try:
        importlib.import_module('transformers.models.qwen3_5.modeling_qwen3_5')
    finally:
        if previous is ...:
            sys.modules.pop('fla', None)
        else:
            sys.modules['fla'] = previous
