from __future__ import annotations

from dataclasses import dataclass

import numpy as np
import torch
from torch import nn


@dataclass
class SequenceModelArtifact:
    state_dict: dict[str, torch.Tensor]
    tabular_mean: np.ndarray
    tabular_std: np.ndarray
    target_mean: float
    target_std: float
    best_epochs: int
    trained_device: str


class TelemetryTransformer(nn.Module):
    def __init__(
        self,
        tabular_features: int,
        sequence_features: int,
        d_model: int = 32,
        n_heads: int = 4,
        n_layers: int = 2,
    ) -> None:
        super().__init__()
        self.sequence_projection = nn.Linear(sequence_features, d_model)
        self.class_token = nn.Parameter(torch.zeros(1, 1, d_model))
        self.position_embedding = nn.Parameter(
            torch.randn(1, 33, d_model) * 0.02
        )
        encoder_layer = nn.TransformerEncoderLayer(
            d_model=d_model,
            nhead=n_heads,
            dim_feedforward=d_model * 2,
            dropout=0.1,
            activation="gelu",
            batch_first=True,
            norm_first=True,
        )
        self.sequence_encoder = nn.TransformerEncoder(
            encoder_layer,
            num_layers=n_layers,
            enable_nested_tensor=False,
        )
        self.tabular_encoder = nn.Sequential(
            nn.Linear(tabular_features, 32),
            nn.GELU(),
            nn.LayerNorm(32),
            nn.Dropout(0.1),
        )
        self.regressor = nn.Sequential(
            nn.Linear(d_model + 32, 32),
            nn.GELU(),
            nn.Dropout(0.1),
            nn.Linear(32, 1),
        )

    def forward(
        self,
        tabular: torch.Tensor,
        sequence: torch.Tensor,
        padding_mask: torch.Tensor,
    ) -> torch.Tensor:
        batch_size = sequence.shape[0]
        projected = self.sequence_projection(sequence)
        class_token = self.class_token.expand(batch_size, -1, -1)
        encoded_input = torch.cat((class_token, projected), dim=1)
        encoded_input = encoded_input + self.position_embedding[
            :, : encoded_input.shape[1]
        ]
        class_padding = torch.zeros(
            (batch_size, 1), dtype=torch.bool, device=padding_mask.device
        )
        encoded = self.sequence_encoder(
            encoded_input,
            src_key_padding_mask=torch.cat((class_padding, padding_mask), dim=1),
        )
        fused = torch.cat(
            (encoded[:, 0], self.tabular_encoder(tabular)),
            dim=1,
        )
        return self.regressor(fused).squeeze(1)


def select_torch_device(requested: str = "auto") -> torch.device:
    if requested not in {"auto", "cpu", "cuda"}:
        raise ValueError("device must be one of: auto, cpu, cuda")
    if requested == "cuda" and not torch.cuda.is_available():
        raise RuntimeError(
            "CUDA was requested, but this PyTorch installation cannot access a CUDA GPU"
        )
    if requested == "cpu":
        return torch.device("cpu")
    return torch.device("cuda" if torch.cuda.is_available() else "cpu")


def sequence_padding_mask(sequences: np.ndarray) -> np.ndarray:
    return np.all(sequences == 0, axis=2)


def _tensor_batch(
    tabular: np.ndarray,
    sequences: np.ndarray,
    padding_mask: np.ndarray,
    indices: np.ndarray,
    device: torch.device,
) -> tuple[torch.Tensor, torch.Tensor, torch.Tensor]:
    return (
        torch.as_tensor(tabular[indices], dtype=torch.float32, device=device),
        torch.as_tensor(sequences[indices], dtype=torch.float32, device=device),
        torch.as_tensor(padding_mask[indices], dtype=torch.bool, device=device),
    )


def _predict_arrays(
    model: TelemetryTransformer,
    tabular: np.ndarray,
    sequences: np.ndarray,
    padding_mask: np.ndarray,
    device: torch.device,
    batch_size: int = 256,
) -> np.ndarray:
    model.eval()
    predictions: list[np.ndarray] = []
    with torch.inference_mode():
        for start in range(0, len(tabular), batch_size):
            indices = np.arange(start, min(start + batch_size, len(tabular)))
            tab, seq, mask = _tensor_batch(
                tabular, sequences, padding_mask, indices, device
            )
            predictions.append(model(tab, seq, mask).cpu().numpy())
    if not predictions:
        return np.empty(0, dtype=np.float32)
    return np.concatenate(predictions)


def fit_sequence_model(
    train_tabular: np.ndarray,
    train_sequences: np.ndarray,
    train_targets: np.ndarray,
    val_tabular: np.ndarray,
    val_sequences: np.ndarray,
    val_targets: np.ndarray,
    *,
    device: str = "auto",
    max_epochs: int = 100,
    patience: int = 12,
    seed: int = 42,
) -> tuple[SequenceModelArtifact, np.ndarray, float]:
    selected_device = select_torch_device(device)
    torch.manual_seed(seed)
    if selected_device.type == "cuda":
        torch.cuda.manual_seed_all(seed)
    torch.set_num_threads(min(4, torch.get_num_threads()))

    tabular_mean = train_tabular.mean(axis=0)
    tabular_std = train_tabular.std(axis=0)
    tabular_std[tabular_std < 1e-6] = 1.0
    train_x = ((train_tabular - tabular_mean) / tabular_std).astype(np.float32)
    val_x = ((val_tabular - tabular_mean) / tabular_std).astype(np.float32)
    train_masks = sequence_padding_mask(train_sequences)
    val_masks = sequence_padding_mask(val_sequences)

    model = TelemetryTransformer(
        tabular_features=train_tabular.shape[1],
        sequence_features=train_sequences.shape[2],
    ).to(selected_device)
    optimizer = torch.optim.AdamW(model.parameters(), lr=0.001, weight_decay=0.01)
    criterion = nn.SmoothL1Loss()
    target_mean = float(train_targets.mean())
    target_std = float(train_targets.std())
    if target_std < 1e-6:
        target_std = 1.0
    normalized_train_targets = (
        (train_targets - target_mean) / target_std
    ).astype(np.float32)
    normalized_val_targets = (
        (val_targets - target_mean) / target_std
    ).astype(np.float32)

    best_state: dict[str, torch.Tensor] | None = None
    best_val_mae = float("inf")
    best_epoch = 1
    epochs_without_improvement = 0
    rng = np.random.default_rng(seed)
    batch_size = min(128, len(train_targets))

    for epoch in range(1, max_epochs + 1):
        model.train()
        shuffled = rng.permutation(len(train_targets))
        for start in range(0, len(shuffled), batch_size):
            indices = shuffled[start : start + batch_size]
            tab, seq, mask = _tensor_batch(
                train_x, train_sequences, train_masks, indices, selected_device
            )
            targets = torch.as_tensor(
                normalized_train_targets[indices],
                dtype=torch.float32,
                device=selected_device,
            )
            optimizer.zero_grad(set_to_none=True)
            loss = criterion(model(tab, seq, mask), targets)
            loss.backward()
            nn.utils.clip_grad_norm_(model.parameters(), max_norm=1.0)
            optimizer.step()

        normalized_predictions = _predict_arrays(
            model, val_x, val_sequences, val_masks, selected_device
        )
        predictions = normalized_predictions * target_std + target_mean
        val_mae = float(np.mean(np.abs(val_targets - predictions)))
        if val_mae < best_val_mae:
            best_val_mae = val_mae
            best_epoch = epoch
            best_state = {
                name: tensor.detach().cpu().clone()
                for name, tensor in model.state_dict().items()
            }
            epochs_without_improvement = 0
        else:
            epochs_without_improvement += 1
            if epochs_without_improvement >= patience:
                break

    if best_state is None:
        raise RuntimeError("Transformer training did not produce a valid checkpoint")
    model.load_state_dict(best_state)
    artifact = SequenceModelArtifact(
        state_dict=best_state,
        tabular_mean=tabular_mean.astype(np.float32),
        tabular_std=tabular_std.astype(np.float32),
        target_mean=target_mean,
        target_std=target_std,
        best_epochs=best_epoch,
        trained_device=selected_device.type,
    )
    best_predictions = _predict_arrays(
        model, val_x, val_sequences, val_masks, selected_device
    )
    best_predictions = best_predictions * target_std + target_mean
    return artifact, best_predictions, best_val_mae


def fit_sequence_model_fixed_epochs(
    tabular: np.ndarray,
    sequences: np.ndarray,
    targets: np.ndarray,
    *,
    epochs: int,
    device: str = "auto",
    seed: int = 42,
) -> SequenceModelArtifact:
    selected_device = select_torch_device(device)
    torch.manual_seed(seed)
    if selected_device.type == "cuda":
        torch.cuda.manual_seed_all(seed)
    torch.set_num_threads(min(4, torch.get_num_threads()))

    tabular_mean = tabular.mean(axis=0)
    tabular_std = tabular.std(axis=0)
    tabular_std[tabular_std < 1e-6] = 1.0
    normalized_tabular = ((tabular - tabular_mean) / tabular_std).astype(np.float32)
    target_mean = float(targets.mean())
    target_std = float(targets.std())
    if target_std < 1e-6:
        target_std = 1.0
    normalized_targets = ((targets - target_mean) / target_std).astype(np.float32)
    padding_mask = sequence_padding_mask(sequences)

    model = TelemetryTransformer(
        tabular_features=tabular.shape[1],
        sequence_features=sequences.shape[2],
    ).to(selected_device)
    optimizer = torch.optim.AdamW(model.parameters(), lr=0.001, weight_decay=0.01)
    criterion = nn.SmoothL1Loss()
    batch_size = min(128, len(targets))
    rng = np.random.default_rng(seed)

    for _ in range(max(1, epochs)):
        model.train()
        shuffled = rng.permutation(len(targets))
        for start in range(0, len(targets), batch_size):
            indices = shuffled[start : start + batch_size]
            tab, seq, mask = _tensor_batch(
                normalized_tabular, sequences, padding_mask, indices, selected_device
            )
            target_batch = torch.as_tensor(
                normalized_targets[indices],
                dtype=torch.float32,
                device=selected_device,
            )
            optimizer.zero_grad(set_to_none=True)
            loss = criterion(model(tab, seq, mask), target_batch)
            loss.backward()
            nn.utils.clip_grad_norm_(model.parameters(), max_norm=1.0)
            optimizer.step()

    return SequenceModelArtifact(
        state_dict={
            name: tensor.detach().cpu().clone()
            for name, tensor in model.state_dict().items()
        },
        tabular_mean=tabular_mean.astype(np.float32),
        tabular_std=tabular_std.astype(np.float32),
        target_mean=target_mean,
        target_std=target_std,
        best_epochs=max(1, epochs),
        trained_device=selected_device.type,
    )


def predict_sequence_artifact(
    artifact: SequenceModelArtifact,
    tabular: np.ndarray,
    sequences: np.ndarray,
    *,
    device: str = "auto",
) -> np.ndarray:
    selected_device = select_torch_device(device)
    model = TelemetryTransformer(
        tabular_features=tabular.shape[1],
        sequence_features=sequences.shape[2],
    ).to(selected_device)
    model.load_state_dict(artifact.state_dict)
    normalized_tabular = (
        (tabular - artifact.tabular_mean) / artifact.tabular_std
    ).astype(np.float32)
    predictions = _predict_arrays(
        model,
        normalized_tabular,
        sequences,
        sequence_padding_mask(sequences),
        selected_device,
    )
    return predictions * artifact.target_std + artifact.target_mean


def restore_sequence_model(
    artifact: SequenceModelArtifact,
    tabular_features: int,
    sequence_features: int,
    device: str,
) -> tuple[TelemetryTransformer, torch.device]:
    selected_device = select_torch_device(device)
    model = TelemetryTransformer(tabular_features, sequence_features).to(
        selected_device
    )
    model.load_state_dict(artifact.state_dict)
    model.eval()
    return model, selected_device


def predict_restored_sequence_model(
    model: TelemetryTransformer,
    device: torch.device,
    artifact: SequenceModelArtifact,
    tabular: np.ndarray,
    sequences: np.ndarray,
) -> np.ndarray:
    normalized_tabular = (
        (tabular - artifact.tabular_mean) / artifact.tabular_std
    ).astype(np.float32)
    predictions = _predict_arrays(
        model,
        normalized_tabular,
        sequences,
        sequence_padding_mask(sequences),
        device,
    )
    return predictions * artifact.target_std + artifact.target_mean
