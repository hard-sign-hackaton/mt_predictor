# syntax=docker/dockerfile:1

# Build context is this directory (the one holding requirements.txt).
#   docker build -t mt-predictor-grpc .
#   docker run --rm -p 50051:50051 mt-predictor-grpc

ARG PYTHON_VERSION=3.12

# CPU-only PyTorch keeps the image small. For NVIDIA, build with:
#   --build-arg TORCH_INDEX_URL=https://download.pytorch.org/whl/cu128
#   --build-arg TORCH_SUFFIX=+cu128 --runtime=nvidia
ARG TORCH_INDEX_URL=https://download.pytorch.org/whl/cpu
ARG TORCH_SUFFIX

FROM python:${PYTHON_VERSION}-slim-bookworm AS builder

ARG TORCH_INDEX_URL
ARG TORCH_SUFFIX
ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH"

RUN python -m venv "$VIRTUAL_ENV"

WORKDIR /build
COPY requirements.txt ./
RUN pip install --index-url "$TORCH_INDEX_URL" "torch>=2.9,<3${TORCH_SUFFIX}" \
    && pip install -r requirements.txt


FROM python:${PYTHON_VERSION}-slim-bookworm AS runtime

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:$PATH" \
    GRPC_HOST=0.0.0.0 \
    GRPC_PORT=50051 \
    GRPC_MAX_WORKERS=8 \
    MODEL_DEVICE=auto \
    LOG_LEVEL=INFO

# libgomp1 is required by the CatBoost shared object; slim images omit it.
RUN apt-get update \
    && apt-get install --no-install-recommends -y libgomp1 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 delay

COPY --from=builder /opt/venv /opt/venv

WORKDIR /app
COPY --chown=delay:delay delay_service ./delay_service

USER delay
EXPOSE 50051

# The port is bound only after the model artifact is loaded, so this check
# also covers a slow or failing model restore.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["python", "-c", "import os, socket; socket.create_connection(('127.0.0.1', int(os.environ.get('GRPC_PORT', '50051'))), 3).close()"]

CMD ["python", "-m", "delay_service.grpc_server"]
