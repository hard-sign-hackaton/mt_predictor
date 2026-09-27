from __future__ import annotations

import os
import tempfile
from pathlib import Path
from typing import Any

import joblib


def publish_model_artifact(bundle: dict[str, Any], model_path: Path) -> None:
    """Write a complete model artifact and atomically publish it at model_path."""
    model_path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            dir=model_path.parent,
            prefix=f".{model_path.name}.",
            suffix=".tmp",
            delete=False,
        ) as temporary_file:
            temporary_path = Path(temporary_file.name)
        joblib.dump(bundle, temporary_path)
        os.replace(temporary_path, model_path)
    finally:
        if temporary_path is not None:
            temporary_path.unlink(missing_ok=True)
