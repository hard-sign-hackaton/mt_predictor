# MT Predictor

Standalone gRPC service for predicting transit stop delays. This repository
contains the service source, generated Protobuf/gRPC bindings, tests, dependency
manifest, and a ready-to-use CatBoost + Transformer model dump. Training data
is intentionally not included.

The checked-in model is about 1.2 MB. It was trained with CUDA, but its weights
are stored independently of the GPU and can be loaded on CPU. Inference uses
CUDA automatically when available; Apple MPS is not currently implemented, so
Apple Silicon runs the Transformer on CPU.

## Install

Create an environment from the repository root:

```powershell
py -m venv .venv
.\.venv\Scripts\python.exe -m pip install -r requirements.txt
```

On Linux or macOS:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
```

### Optional NVIDIA CUDA

For Windows with a CUDA 12.8-compatible NVIDIA driver, replace the CPU/default
PyTorch wheel with the CUDA build:

```powershell
.\.venv\Scripts\python.exe -m pip install --force-reinstall --index-url https://download.pytorch.org/whl/cu128 "torch>=2.9,<3"
.\.venv\Scripts\python.exe -c "import torch; print(torch.cuda.is_available(), torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'CPU')"
```

The gRPC service works without an NVIDIA GPU. It falls back to CPU. Set
`MODEL_DEVICE=cuda` to require CUDA, or `MODEL_DEVICE=cpu` to force CPU.

## Run gRPC server

From the repository root:

```powershell
.\.venv\Scripts\python.exe -m delay_service.grpc_server --host 0.0.0.0 --port 50051
```

On Linux or macOS:

```bash
.venv/bin/python -m delay_service.grpc_server --host 0.0.0.0 --port 50051
```

The model dump is loaded from `delay_service/artifacts/delay_model.joblib`.
Override its location using `DELAY_MODEL_PATH`. Server settings can be supplied
with `GRPC_HOST`, `GRPC_PORT`, and `GRPC_MAX_WORKERS`.

The Protobuf service is `delay_service.v1.DelayPredictionService`:

- `Predict(PredictRequest) returns (PredictionResponse)`
- `PredictBatch(BatchPredictRequest) returns (BatchPredictionResponse)`

The contract is in [`delay_service/proto/delay_service.proto`](delay_service/proto/delay_service.proto).
Send telemetry records with their `tr_id`; records later than the forecast time
`T` are ignored.

## Client example

```python
from datetime import datetime, timezone

import grpc
from google.protobuf.timestamp_pb2 import Timestamp
from delay_service.proto import delay_service_pb2 as pb
from delay_service.proto import delay_service_pb2_grpc as pb_grpc

def timestamp(value):
    result = Timestamp()
    result.FromDatetime(datetime.fromisoformat(value).replace(tzinfo=timezone.utc))
    return result

request = pb.PredictRequest()
request.point.sample_id = "131672_1767670500"
request.point.tr_id = 131672
request.point.T.CopyFrom(timestamp("2026-01-06T03:35:00"))
request.point.target_stop_id = 53700172828
request.point.target_time_begin.CopyFrom(timestamp("2026-01-06T03:50:00"))
request.point.cur_dev_s = 274.0

telemetry = request.telemetry.add()
telemetry.tr_id = 131672
telemetry.event_time.CopyFrom(timestamp("2026-01-06T03:34:50"))
telemetry.location_valid = True
telemetry.lon = 37.5
telemetry.lat = 55.7
telemetry.speed = 24.0
telemetry.heading = 180.0

with grpc.insecure_channel("localhost:50051") as channel:
    stub = pb_grpc.DelayPredictionServiceStub(channel)
    response = stub.Predict(request, timeout=3)
    print(response.sample_id, response.prediction, response.unit)
```

Use TLS credentials when connecting across an untrusted network.

## Retrain and regenerate submission (optional)

Training requires the original dataset layout (`train/`, `test/`, `validate/`,
and `labels/`) alongside this repository, or pass its location with
`--data-dir`. The trained ensemble and validation `submission.csv` are written
to the paths specified by the command:

```powershell
.\.venv\Scripts\python.exe -m delay_service.train --data-dir "C:\path\to\dataset" --device auto
```

Use `--device cuda` to require NVIDIA CUDA or `--device cpu` to force CPU.
Training compares on the labeled test set; it does not fit on test labels.

## Tests

From the repository root:

```powershell
.\.venv\Scripts\python.exe -m unittest discover -s tests -v
```

## Files

- `delay_service/` — service, model code, Protobuf contract/bindings, and model.
- `tests/` — feature, model, and live in-process gRPC tests.
- `requirements.txt` — runtime and training dependencies.
- `.gitignore` — excludes local environments, caches, and copied dataset files.
