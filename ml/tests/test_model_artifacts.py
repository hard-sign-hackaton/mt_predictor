from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import joblib

from delay_service.model_artifacts import publish_model_artifact


class ModelArtifactPublishingTests(unittest.TestCase):
    def test_publishes_complete_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            model_path = Path(directory) / "versions" / "model-v1.joblib"

            publish_model_artifact({"model_version": "v1"}, model_path)

            self.assertEqual(joblib.load(model_path), {"model_version": "v1"})
            self.assertEqual(list(model_path.parent.iterdir()), [model_path])

    def test_failed_write_preserves_existing_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            model_path = Path(directory) / "model.joblib"
            publish_model_artifact({"model_version": "old"}, model_path)

            with patch(
                "delay_service.model_artifacts.joblib.dump",
                side_effect=OSError("simulated disk failure"),
            ):
                with self.assertRaisesRegex(OSError, "simulated disk failure"):
                    publish_model_artifact({"model_version": "new"}, model_path)

            self.assertEqual(joblib.load(model_path), {"model_version": "old"})
            self.assertEqual(list(model_path.parent.iterdir()), [model_path])


if __name__ == "__main__":
    unittest.main()
