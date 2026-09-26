from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Mapping, Sequence

import joblib
import numpy as np
import pandas as pd

from .features import (
    DEFAULT_SEQUENCE_LENGTH,
    FEATURE_NAMES,
    SEQUENCE_FEATURE_NAMES,
    make_training_features,
    make_training_sequences,
)
from .sequence_model import (
    SequenceModelArtifact,
    predict_restored_sequence_model,
    restore_sequence_model,
    select_torch_device,
)


DEFAULT_MODEL_PATH = Path(__file__).parent / "artifacts" / "delay_model.joblib"


class DelayPredictor:
    def __init__(self, model_path: str | Path | None = None) -> None:
        self.model_path = Path(
            model_path or os.environ.get("DELAY_MODEL_PATH", DEFAULT_MODEL_PATH)
        )
        if not self.model_path.is_file():
            raise FileNotFoundError(
                f"Model artifact not found: {self.model_path}. "
                "Train it with `python -m delay_service.train` first."
            )
        bundle = joblib.load(self.model_path)
        if bundle.get("feature_names") != FEATURE_NAMES:
            raise ValueError(f"Model feature schema does not match {self.model_path}")
        if bundle.get("model_name") != "catboost_transformer_ensemble":
            raise ValueError(
                f"Model artifact {self.model_path} is not the CatBoost/Transformer "
                "ensemble. Retrain it with `python -m delay_service.train`."
            )
        if bundle.get("sequence_feature_names") != SEQUENCE_FEATURE_NAMES:
            raise ValueError(f"Sequence schema does not match {self.model_path}")
        if bundle.get("sequence_length") != DEFAULT_SEQUENCE_LENGTH:
            raise ValueError(f"Sequence length does not match {self.model_path}")
        self.catboost_model = bundle["catboost_model"]
        self.transformer_artifact: SequenceModelArtifact = bundle[
            "transformer_model"
        ]
        self.catboost_weight = float(bundle["catboost_weight"])
        self.device = select_torch_device(os.environ.get("MODEL_DEVICE", "auto"))
        self.transformer_model, self.device = restore_sequence_model(
            self.transformer_artifact,
            tabular_features=len(FEATURE_NAMES),
            sequence_features=len(SEQUENCE_FEATURE_NAMES),
            device=self.device.type,
        )
        self.training_device = bundle.get("transformer_training_device", "unknown")

    def predict_one(
        self,
        point: Mapping[str, Any],
        telemetry: Sequence[Mapping[str, Any]],
    ) -> float:
        return self.predict_many([point], telemetry)[0]

    def predict_many(
        self,
        points: Sequence[Mapping[str, Any]],
        telemetry: Sequence[Mapping[str, Any]],
    ) -> list[float]:
        if not points:
            return []
        vehicle_ids = {int(point["tr_id"]) for point in points}
        points_frame = pd.DataFrame(list(points))
        telemetry_rows: list[dict[str, Any]] = []
        for row in telemetry:
            telemetry_row = dict(row)
            telemetry_vehicle_id = telemetry_row.get("tr_id")
            if telemetry_vehicle_id is None:
                if len(vehicle_ids) > 1:
                    raise ValueError(
                        "Telemetry tr_id is required when batch points contain "
                        "multiple vehicles"
                    )
                telemetry_row["tr_id"] = next(iter(vehicle_ids))
            telemetry_rows.append(telemetry_row)
        telemetry_frame = pd.DataFrame(
            telemetry_rows,
            columns=[
                "tr_id",
                "event_time",
                "location_valid",
                "lon",
                "lat",
                "alt",
                "speed",
                "heading",
            ],
        )
        features = make_training_features(points_frame, telemetry_frame)
        sequences = make_training_sequences(points_frame, telemetry_frame)
        tabular_values = features.to_numpy(dtype=np.float32)
        catboost_predictions = np.asarray(
            self.catboost_model.predict(features), dtype=float
        )
        transformer_predictions = predict_restored_sequence_model(
            self.transformer_model,
            self.device,
            self.transformer_artifact,
            tabular_values,
            sequences,
        )
        predictions = (
            self.catboost_weight * catboost_predictions
            + (1.0 - self.catboost_weight) * transformer_predictions
        )
        if not np.isfinite(predictions).all():
            raise ValueError("Ensemble returned a non-finite prediction")
        return predictions.tolist()
