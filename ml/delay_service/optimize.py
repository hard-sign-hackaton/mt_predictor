from __future__ import annotations

import argparse
from pathlib import Path
from typing import Sequence

import joblib

from .onnx_runtime import export_transformer_onnx
from .predictor import DEFAULT_MODEL_PATH
from .features import (
    DEFAULT_SEQUENCE_LENGTH,
    FEATURE_NAMES,
    SEQUENCE_FEATURE_NAMES,
)
from .sequence_model import restore_sequence_model


def main(argv: Sequence[str] | None = None) -> None:
    parser = argparse.ArgumentParser(
        description="Export the Transformer to an optimized ONNX Runtime model"
    )
    parser.add_argument(
        "--model-path",
        type=Path,
        default=DEFAULT_MODEL_PATH,
        help="Trained CatBoost/Transformer model artifact",
    )
    parser.add_argument(
        "--output",
        type=Path,
        help="Output ONNX path; defaults beside model-path",
    )
    parser.add_argument(
        "--quantize-int8",
        action="store_true",
        help="Dynamically quantize ONNX weights to INT8 (experimental; benchmark quality)",
    )
    args = parser.parse_args(argv)

    model_path = args.model_path
    if not model_path.is_file():
        raise FileNotFoundError(f"Model artifact not found: {model_path}")
    output_path = args.output
    if output_path is None:
        suffix = ".int8.onnx" if args.quantize_int8 else ".onnx"
        output_path = model_path.with_suffix(suffix)

    bundle = joblib.load(model_path)
    if bundle.get("model_name") != "catboost_transformer_ensemble":
        raise ValueError(f"Unsupported model artifact: {model_path}")
    if bundle.get("feature_names") != FEATURE_NAMES:
        raise ValueError(f"Model feature schema does not match {model_path}")
    if bundle.get("sequence_feature_names") != SEQUENCE_FEATURE_NAMES:
        raise ValueError(f"Sequence feature schema does not match {model_path}")
    if bundle.get("sequence_length") != DEFAULT_SEQUENCE_LENGTH:
        raise ValueError(f"Sequence length does not match {model_path}")
    transformer_artifact = bundle["transformer_model"]
    transformer_model, _ = restore_sequence_model(
        transformer_artifact,
        tabular_features=len(bundle["feature_names"]),
        sequence_features=len(bundle["sequence_feature_names"]),
        device="cpu",
    )
    maximum_error, mean_error = export_transformer_onnx(
        transformer_model,
        transformer_artifact,
        output_path,
        quantize_int8=args.quantize_int8,
    )
    print(f"ONNX model written: {output_path}")
    print(
        "PyTorch/ONNX parity on synthetic batches: "
        f"max_abs_error={maximum_error:.6f} sec, "
        f"mean_abs_error={mean_error:.6f} sec"
    )
    if args.quantize_int8:
        print(
            "INT8 is experimental: evaluate MAE on a held-out labeled dataset "
            "before deployment."
        )


if __name__ == "__main__":
    main()
