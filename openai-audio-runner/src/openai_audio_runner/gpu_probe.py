"""Startup GPU probe: fail fast, and say why.

Two checks when DEVICE=cuda:
  1. a CUDA device is visible at all;
  2. the torch build in this image carries kernels for that device's
     compute capability. The default images install torch from the cu128
     wheel index, which ships sm_75+ only — a Pascal card (sm_61, e.g. a
     GTX 1080) passes check 1 and then fails on the first kernel launch.
     The `-pascal` image flavor (cu126 wheels) is the fix; this probe
     names it instead of letting the runner die mid-request.
"""

import sys


def _arch_supported(archs: list[str], major: int, minor: int) -> bool:
    """A device runs SASS for its own major with minor <= its minor, and
    JIT-compiles PTX embedded for any arch <= its own."""
    device = major * 10 + minor
    for arch in archs:
        try:
            kind, num = arch.split("_", 1)
            value = int(num)
        except ValueError:
            continue
        if kind == "sm" and value // 10 == major and value % 10 <= minor:
            return True
        if kind == "compute" and value <= device:
            return True
    return False


def fail_fast_if_cuda_requested_without_gpu(device: str) -> None:
    if device != "cuda":
        return
    try:
        import torch
    except ImportError:
        sys.stderr.write(
            "torch not installed; cannot probe for CUDA. Install torch or set DEVICE=cpu.\n"
        )
        sys.exit(1)
    if not torch.cuda.is_available():
        sys.stderr.write(
            "cuda device requested but no GPU detected; "
            "set DEVICE=cpu to fall back to CPU runtime\n"
        )
        sys.exit(1)

    major, minor = torch.cuda.get_device_capability(0)
    archs = list(torch.cuda.get_arch_list())
    name = torch.cuda.get_device_name(0)
    if archs and not _arch_supported(archs, major, minor):
        sys.stderr.write(
            f"GPU {name!r} is compute capability sm_{major}{minor}, but this torch build "
            f"only carries {archs}. Use the -pascal image flavor (cu126 wheels) for "
            f"sm_6x cards, or a newer GPU.\n"
        )
        sys.exit(1)
    sys.stderr.write(f"GPU probe ok: {name} sm_{major}{minor}; torch archs {archs}\n")
