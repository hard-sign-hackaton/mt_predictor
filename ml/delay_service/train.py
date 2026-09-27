from __future__ import annotations

import argparse
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
import pandas as pd
from catboost import CatBoostRegressor
from sklearn.metrics import mean_absolute_error
from sklearn.model_selection import GroupShuffleSplit

from .model_artifacts import publish_model_artifact
from .features import (
    DEFAULT_SEQUENCE_LENGTH,
    FEATURE_NAMES,
    SEQUENCE_FEATURE_NAMES,
    make_training_features,
    make_training_sequences,
)
from .sequence_model import (
    fit_sequence_model,
    fit_sequence_model_fixed_epochs,
    predict_sequence_artifact,
    select_torch_device,
)


def load_dataset(data_dir: Path, split: str) -> tuple[pd.DataFrame, pd.DataFrame]:
    labels_path = data_dir / "labels" / f"labels_{split}.csv"
    traffic_path = data_dir / split / "traffic.csv"
    labels = pd.read_csv(labels_path, parse_dates=["T", "target_time_begin"])
    traffic = pd.read_csv(traffic_path, parse_dates=["event_time"], low_memory=False)
    return labels, traffic


def _choose_catboost_weight(
    catboost_predictions: np.ndarray,
    transformer_predictions: np.ndarray,
    targets: np.ndarray,
) -> tuple[float, float]:
    best_weight = 0.5
    best_mae = float("inf")
    for catboost_weight in np.arange(0.15, 0.851, 0.01):
        predictions = (
            catboost_weight * catboost_predictions
            + (1.0 - catboost_weight) * transformer_predictions
        )
        mae = mean_absolute_error(targets, predictions)
        if mae < best_mae:
            best_weight = float(catboost_weight)
            best_mae = float(mae)
    return best_weight, best_mae


def _new_catboost() -> CatBoostRegressor:
    return CatBoostRegressor(
        iterations=1000,
        depth=6,
        learning_rate=0.03,
        loss_function="MAE",
        eval_metric="MAE",
        random_seed=42,
        verbose=False,
        allow_writing_files=False,
    )


def fit_and_evaluate(
    data_dir: Path,
    model_out: Path,
    submission_out: Path | None,
    device: str = "auto",
) -> None:
    selected_device = select_torch_device(device)
    train_labels, train_traffic = load_dataset(data_dir, "train")
    test_labels, test_traffic = load_dataset(data_dir, "test")

    x_train = make_training_features(train_labels, train_traffic)
    x_test = make_training_features(test_labels, test_traffic)
    seq_train = make_training_sequences(train_labels, train_traffic)
    seq_test = make_training_sequences(test_labels, test_traffic)
    y_train = train_labels["target_delay_s"].to_numpy(dtype=float)
    y_test = test_labels["target_delay_s"].to_numpy(dtype=float)

    split = GroupShuffleSplit(n_splits=1, test_size=0.2, random_state=42)
    fit_indices, blend_indices = next(
        split.split(x_train, y_train, groups=train_labels["tr_id"])
    )
    tuning_catboost = _new_catboost()
    tuning_catboost.fit(x_train.iloc[fit_indices], y_train[fit_indices])
    tuning_catboost_predictions = tuning_catboost.predict(x_train.iloc[blend_indices])
    tuning_transformer, transformer_blend_predictions, transformer_blend_mae = (
        fit_sequence_model(
            x_train.to_numpy(dtype=np.float32)[fit_indices],
            seq_train[fit_indices],
            y_train[fit_indices],
            x_train.to_numpy(dtype=np.float32)[blend_indices],
            seq_train[blend_indices],
            y_train[blend_indices],
            device=selected_device.type,
        )
    )
    catboost_weight, blend_validation_mae = _choose_catboost_weight(
        tuning_catboost_predictions,
        transformer_blend_predictions,
        y_train[blend_indices],
    )
    print(
        f"Transformer device: {tuning_transformer.trained_device}; "
        f"blend holdout MAE={transformer_blend_mae:.2f} sec"
    )
    print(
        f"Group holdout ensemble MAE={blend_validation_mae:.2f} sec; "
        f"CatBoost weight={catboost_weight:.2f}"
    )

    transformer_epochs = tuning_transformer.best_epochs
    catboost_model = _new_catboost()
    catboost_model.fit(x_train, y_train)
    transformer_model = fit_sequence_model_fixed_epochs(
        x_train.to_numpy(dtype=np.float32),
        seq_train,
        y_train,
        epochs=transformer_epochs,
        device=selected_device.type,
    )
    test_catboost_predictions = catboost_model.predict(x_test)
    test_transformer_predictions = predict_sequence_artifact(
        transformer_model,
        x_test.to_numpy(dtype=np.float32),
        seq_test,
        device=selected_device.type,
    )
    test_predictions = (
        catboost_weight * test_catboost_predictions
        + (1.0 - catboost_weight) * test_transformer_predictions
    )
    catboost_test_mae = mean_absolute_error(y_test, test_catboost_predictions)
    transformer_test_mae = mean_absolute_error(y_test, test_transformer_predictions)
    ensemble_test_mae = mean_absolute_error(y_test, test_predictions)
    cur_dev_test_mae = mean_absolute_error(
        y_test, test_labels["cur_dev_s"].to_numpy(dtype=float)
    )
    print(f"cur_dev_baseline: test MAE={cur_dev_test_mae:.2f} sec")
    print(f"catboost: test MAE={catboost_test_mae:.2f} sec")
    print(f"transformer: test MAE={transformer_test_mae:.2f} sec")
    print(f"ensemble: test MAE={ensemble_test_mae:.2f} sec")

    publish_model_artifact(
        {
            "model_name": "catboost_transformer_ensemble",
            "model_version": model_out.stem,
            "created_at_utc": datetime.now(timezone.utc).isoformat(),
            "catboost_model": catboost_model,
            "transformer_model": transformer_model,
            "catboost_weight": catboost_weight,
            "feature_names": FEATURE_NAMES,
            "sequence_feature_names": SEQUENCE_FEATURE_NAMES,
            "sequence_length": DEFAULT_SEQUENCE_LENGTH,
            "test_mae_s": float(ensemble_test_mae),
            "catboost_test_mae_s": float(catboost_test_mae),
            "transformer_test_mae_s": float(transformer_test_mae),
            "blend_validation_mae_s": float(blend_validation_mae),
            "training_rows": len(train_labels),
            "transformer_epochs": transformer_epochs,
            "transformer_training_device": transformer_model.trained_device,
            "preferred_inference_device": selected_device.type,
        },
        model_out,
    )

    validate_count = 0
    if submission_out is not None:
        points_path = data_dir / "validate" / "points.csv"
        validate_traffic_path = data_dir / "validate" / "traffic.csv"
        validate_points = pd.read_csv(
            points_path, parse_dates=["T", "target_time_begin"]
        )
        validate_traffic = pd.read_csv(
            validate_traffic_path, parse_dates=["event_time"], low_memory=False
        )
        x_validate = make_training_features(validate_points, validate_traffic)
        seq_validate = make_training_sequences(validate_points, validate_traffic)
        validate_catboost_predictions = catboost_model.predict(x_validate)
        validate_transformer_predictions = predict_sequence_artifact(
            transformer_model,
            x_validate.to_numpy(dtype=np.float32),
            seq_validate,
            device=selected_device.type,
        )
        validate_predictions = (
            catboost_weight * validate_catboost_predictions
            + (1.0 - catboost_weight) * validate_transformer_predictions
        )
        submission_out.parent.mkdir(parents=True, exist_ok=True)
        pd.DataFrame(
            {
                "sample_id": validate_points["sample_id"],
                "prediction": validate_predictions,
            }
        ).to_csv(submission_out, sep=";", index=False, float_format="%.3f")
        validate_count = len(validate_predictions)

    print("Selected model: CatBoost + telemetry Transformer ensemble")
    print(f"Test MAE: {ensemble_test_mae:.2f} sec")
    print(f"Ensemble weights: CatBoost={catboost_weight:.2f}, Transformer={1-catboost_weight:.2f}")
    print(f"Training/inference device: {selected_device}")
    print(f"Published model version {model_out.stem}: {model_out}")
    if submission_out is not None:
        print(f"Saved {validate_count} validate predictions: {submission_out}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Train and evaluate the delay service")
    parser.add_argument(
        "--data-dir",
        type=Path,
        default=Path.cwd(),
        help="Directory containing train/, test/, and labels/; validate/ is optional",
    )
    parser.add_argument(
        "--model-out",
        type=Path,
        help=(
            "Output path for an immutable model version. If omitted, a "
            "timestamped artifact is written under artifacts/versions/."
        ),
    )
    parser.add_argument(
        "--submission-out",
        type=Path,
        help="Optional output path for predictions on validate/",
    )
    parser.add_argument(
        "--device",
        choices=("auto", "cpu", "cuda"),
        default="auto",
        help="Device for Transformer training and inference (default: auto)",
    )
    args = parser.parse_args()
    model_out = args.model_out
    if model_out is None:
        version = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
        model_out = (
            Path(__file__).parent
            / "artifacts"
            / "versions"
            / f"delay_model-{version}.joblib"
        )
    fit_and_evaluate(args.data_dir, model_out, args.submission_out, args.device)


if __name__ == "__main__":
    main()
