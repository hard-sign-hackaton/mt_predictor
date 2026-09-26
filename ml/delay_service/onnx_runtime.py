from __future__ import annotations

import os
import tempfile
from pathlib import Path

import numpy as np
import torch
from torch import nn

from .features import DEFAULT_SEQUENCE_LENGTH
from .sequence_model import (
    SequenceModelArtifact,
    sequence_padding_mask,
)


class _NormalizedTransformer(nn.Module):
    def __init__(
        self, model: nn.Module, artifact: SequenceModelArtifact
    ) -> None:
        super().__init__()
        self.model = model
        self.register_buffer(
            "tabular_mean",
            torch.as_tensor(artifact.tabular_mean, dtype=torch.float32),
        )
        self.register_buffer(
            "tabular_std",
            torch.as_tensor(artifact.tabular_std, dtype=torch.float32),
        )
        self.target_mean = artifact.target_mean
        self.target_std = artifact.target_std

    def forward(
        self,
        tabular: torch.Tensor,
        sequence: torch.Tensor,
        padding_mask: torch.Tensor,
    ) -> torch.Tensor:
        prediction = self.model(
            (tabular - self.tabular_mean) / self.tabular_std,
            sequence,
            padding_mask,
        )
        return prediction * self.target_std + self.target_mean


def export_transformer_onnx(
    model: nn.Module,
    artifact: SequenceModelArtifact,
    output_path: str | Path,
    *,
    quantize_int8: bool = False,
) -> tuple[float, float]:
    try:
        import onnx
    except ImportError as exc:
        raise RuntimeError(
            "ONNX export requires optional dependencies: "
            "install with `pip install -r requirements-onnx.txt`"
        ) from exc

    output_path = Path(output_path)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=output_path.parent) as temporary_dir:
        temporary_dir_path = Path(temporary_dir)
        float_model_path = temporary_dir_path / "transformer.onnx"
        dummy_tabular = torch.zeros(
            (1, len(artifact.tabular_mean)), dtype=torch.float32
        )
        dummy_sequence = torch.zeros(
            (
                1,
                DEFAULT_SEQUENCE_LENGTH,
                model.sequence_projection.in_features,
            ),
            dtype=torch.float32,
        )
        dummy_mask = torch.zeros((1, DEFAULT_SEQUENCE_LENGTH), dtype=torch.bool)
        export_model = _NormalizedTransformer(model.eval().cpu(), artifact).eval()

        torch.onnx.export(
            export_model,
            (dummy_tabular, dummy_sequence, dummy_mask),
            float_model_path,
            input_names=["tabular", "sequence", "padding_mask"],
            output_names=["prediction"],
            dynamic_axes={
                "tabular": {0: "batch"},
                "sequence": {0: "batch"},
                "padding_mask": {0: "batch"},
                "prediction": {0: "batch"},
            },
            opset_version=17,
            dynamo=False,
        )
        onnx.checker.check_model(str(float_model_path))
        if quantize_int8:
            try:
                from onnxruntime.quantization import QuantType, quantize_dynamic
            except ImportError as exc:
                raise RuntimeError(
                    "INT8 ONNX quantization requires onnxruntime; install with "
                    "`pip install -r requirements-onnx.txt`"
                ) from exc
            published_path = temporary_dir_path / "transformer.int8.onnx"
            quantize_dynamic(
                str(float_model_path),
                str(published_path),
                weight_type=QuantType.QInt8,
            )
            onnx.checker.check_model(str(published_path))
        else:
            published_path = float_model_path

        session = _create_onnx_session(published_path)
        rng = np.random.default_rng(42)
        max_abs_error = 0.0
        mean_abs_error = 0.0
        sample_count = 0
        for batch_size in (1, 16):
            tabular = rng.normal(size=(batch_size, len(artifact.tabular_mean))).astype(
                np.float32
            )
            sequences = rng.normal(
                size=(
                    batch_size,
                    DEFAULT_SEQUENCE_LENGTH,
                    model.sequence_projection.in_features,
                )
            ).astype(np.float32)
            padding_mask = np.zeros(
                (batch_size, DEFAULT_SEQUENCE_LENGTH), dtype=np.bool_
            )
            with torch.inference_mode():
                expected = export_model(
                    torch.from_numpy(tabular),
                    torch.from_numpy(sequences),
                    torch.from_numpy(padding_mask),
                ).numpy()
            actual = session.run(
                ["prediction"],
                {
                    "tabular": tabular,
                    "sequence": sequences,
                    "padding_mask": padding_mask,
                },
            )[0]
            errors = np.abs(expected - actual)
            max_abs_error = max(max_abs_error, float(errors.max(initial=0.0)))
            mean_abs_error += float(errors.sum())
            sample_count += errors.size

        mean_abs_error /= max(1, sample_count)
        os.replace(published_path, output_path)
        return max_abs_error, mean_abs_error


def _create_onnx_session(model_path: str | Path):
    try:
        import onnxruntime as ort
    except ImportError as exc:
        raise RuntimeError(
            "ONNX inference requires onnxruntime; install with "
            "`pip install -r requirements-onnx.txt`"
        ) from exc

    intra_op_threads = int(os.environ.get("ONNX_INTRA_OP_THREADS", "1"))
    inter_op_threads = int(os.environ.get("ONNX_INTER_OP_THREADS", "1"))
    if intra_op_threads < 1 or inter_op_threads < 1:
        raise ValueError("ONNX thread counts must be positive integers")
    options = ort.SessionOptions()
    options.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
    options.intra_op_num_threads = intra_op_threads
    options.inter_op_num_threads = inter_op_threads
    session = ort.InferenceSession(
        str(model_path),
        sess_options=options,
        providers=["CPUExecutionProvider"],
    )
    if session.get_providers() != ["CPUExecutionProvider"]:
        raise RuntimeError("ONNX Runtime did not initialize CPUExecutionProvider")
    return session


class OnnxTransformerRuntime:
    def __init__(self, model_path: str | Path) -> None:
        self.model_path = Path(model_path)
        if not self.model_path.is_file():
            raise FileNotFoundError(
                f"ONNX model not found: {self.model_path}. "
                "Export it with `python -m delay_service.optimize`."
            )
        self.session = _create_onnx_session(self.model_path)

    def predict(
        self,
        tabular: np.ndarray,
        sequences: np.ndarray,
    ) -> np.ndarray:
        if len(tabular) == 0:
            return np.empty(0, dtype=np.float32)
        tabular_values = np.ascontiguousarray(tabular, dtype=np.float32)
        sequence_values = np.ascontiguousarray(sequences, dtype=np.float32)
        padding_mask = np.ascontiguousarray(
            sequence_padding_mask(sequence_values), dtype=np.bool_
        )
        predictions = self.session.run(
            ["prediction"],
            {
                "tabular": tabular_values,
                "sequence": sequence_values,
                "padding_mask": padding_mask,
            },
        )[0]
        if not np.isfinite(predictions).all():
            raise ValueError("ONNX Transformer returned a non-finite prediction")
        return predictions
