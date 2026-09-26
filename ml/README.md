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
with `GRPC_HOST`, `GRPC_PORT`, and `GRPC_MAX_WORKERS`. Each server process loads
one immutable model version at startup and is otherwise stateless, so it can be
run as a replica behind a gRPC-capable load balancer.

### Docker

The build context is the directory holding `requirements.txt`:

```bash
docker build -t mt-predictor-grpc .
docker run --rm -p 50051:50051 mt-predictor-grpc
```

The image installs the CPU-only PyTorch wheel, runs as an unprivileged user, and
serves the checked-in model on port 50051. Build for NVIDIA with:

```bash
docker build -t mt-predictor-grpc \
  --build-arg TORCH_INDEX_URL=https://download.pytorch.org/whl/cu128 \
  --build-arg TORCH_SUFFIX=+cu128 .
docker run --rm --gpus all -e MODEL_DEVICE=cuda -p 50051:50051 mt-predictor-grpc
```

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

## Retrain and publish a model version (optional)

Training requires the original dataset layout (`train/`, `test/`, `validate/`,
and `labels/`) alongside this repository, or pass its location with
`--data-dir`. The trained ensemble and validation `submission.csv` are written
to the paths specified by the command. If `--model-out` is omitted, training
publishes a timestamped artifact under `delay_service/artifacts/versions/`.
Training uses the labeled training split; test labels are only used for
evaluation. For retraining with new labels, build an updated cumulative dataset
with a fresh vehicle-disjoint test split, then train and evaluate a new version:

```powershell
.\.venv\Scripts\python.exe -m delay_service.train --data-dir "C:\path\to\dataset" --device auto
```

Use `--device cuda` to require NVIDIA CUDA or `--device cpu` to force CPU.
To select an explicit immutable version path, pass `--model-out`, for example:

```powershell
.\.venv\Scripts\python.exe -m delay_service.train --data-dir "C:\path\to\dataset" --model-out "C:\models\delay_model-v2.joblib"
```

Artifact publication uses a temporary file and atomic replacement, so a failed
write cannot leave a truncated model at the published path. The running server
does not hot-reload artifacts: validate the new version, then roll it out by
restarting/replacing replicas with `DELAY_MODEL_PATH` set to that version. Keep
the previous artifact available for rollback. Training is an offline full
retrain on accumulated labeled data, not online learning from prediction
requests.

## Horizontal scaling

The Docker image above includes the bundled model. To deploy a new model
version, mount the artifact into each replica and set `DELAY_MODEL_PATH`. Run
multiple replicas behind an external gRPC load balancer; give each replica
enough CPU/RAM, and avoid overcommitting a shared GPU. `PredictBatch` supports
up to 1,000 forecast points per request, and `GRPC_MAX_WORKERS` controls the
per-process request worker pool. Benchmark the target hardware to choose replica
and worker counts; increasing threads alone does not guarantee higher
throughput.

## Tests

From the repository root:

```powershell
.\.venv\Scripts\python.exe -m unittest discover -s tests -v
```

## Files

- `delay_service/` — service, model code, Protobuf contract/bindings, and model.
- `tests/` — feature, model, and live in-process gRPC tests.
- `requirements.txt` — runtime and training dependencies.
- `Dockerfile`, `.dockerignore` — container image for the gRPC service.
- `.gitignore` — excludes local environments, caches, and copied dataset files.
