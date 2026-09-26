import unittest

import numpy as np
import torch

from delay_service.sequence_model import (
    fit_sequence_model,
    fit_sequence_model_fixed_epochs,
    predict_sequence_artifact,
    select_torch_device,
)


class SequenceModelTests(unittest.TestCase):
    def test_cpu_transformer_artifact_round_trip(self) -> None:
        rng = np.random.default_rng(3)
        tabular = rng.normal(size=(24, 5)).astype(np.float32)
        sequences = rng.normal(size=(24, 8, 8)).astype(np.float32)
        targets = (tabular[:, 0] * 20 + sequences[:, -1, 1] * 5).astype(
            np.float32
        )

        artifact, _, _ = fit_sequence_model(
            tabular[:18],
            sequences[:18],
            targets[:18],
            tabular[18:],
            sequences[18:],
            targets[18:],
            device="cpu",
            max_epochs=2,
            patience=2,
        )
        refit_artifact = fit_sequence_model_fixed_epochs(
            tabular,
            sequences,
            targets,
            epochs=artifact.best_epochs,
            device="cpu",
        )
        predictions = predict_sequence_artifact(
            refit_artifact, tabular, sequences, device="cpu"
        )

        self.assertEqual(predictions.shape, (24,))
        self.assertTrue(np.isfinite(predictions).all())

    def test_cuda_device_selection_is_truthful(self) -> None:
        selected = select_torch_device("auto")
        self.assertEqual(selected.type, "cuda" if torch.cuda.is_available() else "cpu")
        if not torch.cuda.is_available():
            with self.assertRaises(RuntimeError):
                select_torch_device("cuda")


if __name__ == "__main__":
    unittest.main()
