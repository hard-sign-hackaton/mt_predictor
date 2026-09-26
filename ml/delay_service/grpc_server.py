from __future__ import annotations

import argparse
import logging
import os
from concurrent import futures
from typing import Sequence

import grpc
from pydantic import ValidationError

from .predictor import DelayPredictor
from .proto import delay_service_pb2_grpc
from .proto import delay_service_pb2
from .schemas import (
    PredictionPoint,
    TelemetryPoint,
    protobuf_point_to_dict,
    protobuf_telemetry_to_dict,
)


LOGGER = logging.getLogger(__name__)
MAX_POINTS = 1000
MAX_TELEMETRY_POINTS = 10000


class DelayPredictionServicer(
    delay_service_pb2_grpc.DelayPredictionServiceServicer
):
    def __init__(self, predictor: DelayPredictor) -> None:
        self.predictor = predictor

    def Predict(self, request, context):
        if not request.HasField("point"):
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "point is required")
        if len(request.telemetry) > MAX_TELEMETRY_POINTS:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT, "Too many telemetry points"
            )

        try:
            point = PredictionPoint.model_validate(
                protobuf_point_to_dict(request.point)
            )
            telemetry = [
                TelemetryPoint.model_validate(protobuf_telemetry_to_dict(row))
                for row in request.telemetry
            ]
        except ValidationError as exc:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))

        try:
            prediction = self.predictor.predict_one(
                point.model_dump(),
                [row.model_dump() for row in telemetry],
            )
        except (KeyError, TypeError, ValueError) as exc:
            LOGGER.exception("Model inference failed for sample_id=%s", point.sample_id)
            context.abort(grpc.StatusCode.INTERNAL, "Prediction failed")

        return delay_service_pb2.PredictionResponse(
            sample_id=point.sample_id or "",
            prediction=prediction,
            unit="seconds",
        )

    def PredictBatch(self, request, context):
        if not request.points:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "At least one prediction point is required",
            )
        if len(request.points) > MAX_POINTS:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT, "Too many prediction points"
            )
        if len(request.telemetry) > MAX_TELEMETRY_POINTS:
            context.abort(
                grpc.StatusCode.INVALID_ARGUMENT, "Too many telemetry points"
            )

        try:
            points = [
                PredictionPoint.model_validate(protobuf_point_to_dict(point))
                for point in request.points
            ]
            telemetry = [
                TelemetryPoint.model_validate(protobuf_telemetry_to_dict(row))
                for row in request.telemetry
            ]
        except ValidationError as exc:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))

        try:
            predictions = self.predictor.predict_many(
                [point.model_dump() for point in points],
                [row.model_dump() for row in telemetry],
            )
        except (KeyError, TypeError, ValueError):
            LOGGER.exception("Batch model inference failed")
            context.abort(grpc.StatusCode.INTERNAL, "Prediction failed")

        response = delay_service_pb2.BatchPredictionResponse()
        for point, prediction in zip(points, predictions):
            response.predictions.add(
                sample_id=point.sample_id or "",
                prediction=prediction,
                unit="seconds",
            )
        return response


def serve(host: str = "0.0.0.0", port: int = 50051) -> None:
    max_workers = int(os.environ.get("GRPC_MAX_WORKERS", "8"))
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=max_workers))
    predictor = DelayPredictor()
    delay_service_pb2_grpc.add_DelayPredictionServiceServicer_to_server(
        DelayPredictionServicer(predictor), server
    )
    bound_port = server.add_insecure_port(f"{host}:{port}")
    if bound_port == 0:
        raise RuntimeError(f"Could not bind gRPC server to {host}:{port}")
    server.start()
    LOGGER.info(
        "gRPC server listening on %s:%s; model_version=%s; Transformer backend=%s; "
        "Transformer inference device=%s (trained on %s)",
        host,
        bound_port,
        predictor.model_version,
        predictor.transformer_backend,
        predictor.device,
        predictor.training_device,
    )
    server.wait_for_termination()


def main(argv: Sequence[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description="Run the delay prediction gRPC API")
    parser.add_argument("--host", default=os.environ.get("GRPC_HOST", "0.0.0.0"))
    parser.add_argument(
        "--port",
        type=int,
        default=int(os.environ.get("GRPC_PORT", "50051")),
    )
    args = parser.parse_args(argv)
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO"))
    serve(args.host, args.port)


if __name__ == "__main__":
    main()
