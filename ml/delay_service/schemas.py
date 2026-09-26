from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class TelemetryPoint(BaseModel):
    event_time: datetime
    tr_id: int | None = None
    location_valid: bool = False
    lon: float | None = None
    lat: float | None = None
    alt: float | None = None
    speed: float | None = None
    heading: float | None = None


class PredictionPoint(BaseModel):
    sample_id: str | None = None
    tr_id: int
    T: datetime
    target_stop_id: int | str | None = None
    target_time_begin: datetime
    cur_dev_s: float


class PredictRequest(BaseModel):
    point: PredictionPoint
    telemetry: list[TelemetryPoint] = Field(max_length=10000)


class BatchPredictRequest(BaseModel):
    points: list[PredictionPoint] = Field(min_length=1, max_length=1000)
    telemetry: list[TelemetryPoint] = Field(max_length=10000)


class PredictionResponse(BaseModel):
    sample_id: str | None = None
    prediction: float
    unit: str = "seconds"


def protobuf_point_to_dict(point: Any) -> dict[str, Any]:
    return {
        "sample_id": point.sample_id or None,
        "tr_id": point.tr_id,
        "T": point.T.ToDatetime() if point.HasField("T") else None,
        "target_stop_id": (
            point.target_stop_id if point.HasField("target_stop_id") else None
        ),
        "target_time_begin": (
            point.target_time_begin.ToDatetime()
            if point.HasField("target_time_begin")
            else None
        ),
        "cur_dev_s": point.cur_dev_s,
    }


def protobuf_telemetry_to_dict(row: Any) -> dict[str, Any]:
    return {
        "event_time": row.event_time.ToDatetime() if row.HasField("event_time") else None,
        "tr_id": row.tr_id if row.HasField("tr_id") else None,
        "location_valid": row.location_valid,
        "lon": row.lon if row.HasField("lon") else None,
        "lat": row.lat if row.HasField("lat") else None,
        "alt": row.alt if row.HasField("alt") else None,
        "speed": row.speed if row.HasField("speed") else None,
        "heading": row.heading if row.HasField("heading") else None,
    }
