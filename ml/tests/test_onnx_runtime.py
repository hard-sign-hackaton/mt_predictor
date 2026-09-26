from importlib.util import find_spec
from pathlib import Path
import tempfile
import unittest

import numpy as np
import torch

from delay_service.onnx_runtime import (
    OnnxTransformerRuntime,
    export_transformer_onnx,
)
from delay_service.sequence_model import (
    SequenceModelArtifact,
    TelemetryTransformer,
    predict_sequence_artifact,
)


@unittest.skipUnless(
    find_spec("onnx") is not None and find_spec("onnxruntime") is not None,
    "install optional ONNX dependencies to run ONNX tests",
)
class OnnxRuntimeTests(unittest.TestCase):
    def test_onnx_predictions_match_pytorch_for_dynamic_batches(self) -> None:
        torch.manual_seed(7)
        model = TelemetryTransformer(tabular_features=5, sequence_features=8).eval()
        artifact = SequenceModelArtifact(
            state_dict={
                name: value.detach().clone()
                for name, value in model.state_dict().items()
            },
            tabular_mean=np.zeros(5, dtype=np.float32),
            tabular_std=np.ones(5, dtype=np.float32),
            target_mean=23.0,
            target_std=17.0,
            best_epochs=1,
            trained_device="cpu",
        )
        with tempfile.TemporaryDirectory() as directory:
            model_path = Path(directory) / "transformer.onnx"
            max_error, _ = export_transformer_onnx(model, artifact, model_path)
            runtime = OnnxTransformerRuntime(model_path)
            for batch_size in (1, 4):
                rng = np.random.default_rng(batch_size)
                tabular = rng.normal(size=(batch_size, 5)).astype(np.float32)
                sequences = rng.normal(size=(batch_size, 32, 8)).astype(np.float32)
                sequences[:, :3] = 0.0

                expected = predict_sequence_artifact(
                    artifact, tabular, sequences, device="cpu"
                )
                actual = runtime.predict(tabular, sequences)

                np.testing.assert_allclose(actual, expected, rtol=1e-5, atol=1e-4)
            self.assertLess(max_error, 1e-4)


if __name__ == "__main__":
    unittest.main()
