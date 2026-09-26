from __future__ import annotations

from bisect import bisect_right
from datetime import datetime, timedelta
from math import cos, radians, sin
from typing import Any, Mapping, Sequence

import numpy as np


FEATURE_NAMES = [
    "cur_dev_s",
    "minutes_to_target",
    "time_of_day_sin",
    "time_of_day_cos",
    "target_time_sin",
    "target_time_cos",
    "telemetry_age_s",
    "telemetry_count_5m",
    "telemetry_count_10m",
    "location_valid",
    "speed_last",
    "speed_mean_5m",
    "speed_std_5m",
    "speed_mean_10m",
    "speed_trend_5m",
    "heading_sin",
    "heading_cos",
    "lon",
    "lat",
    "alt",
]

SEQUENCE_FEATURE_NAMES = [
    "seconds_before_T",
    "speed_scaled",
    "heading_sin",
    "heading_cos",
    "lon_scaled",
    "lat_scaled",
    "alt_scaled",
    "location_valid",
]
DEFAULT_SEQUENCE_LENGTH = 32


def parse_datetime(value: Any) -> datetime:
    if isinstance(value, datetime):
        return value
    if not isinstance(value, str):
        raise ValueError("Timestamp must be a datetime or ISO-8601 string")
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def extract_features(
    point: Mapping[str, Any],
    telemetry: Sequence[Mapping[str, Any]],
) -> dict[str, float]:
    forecast_time = parse_datetime(point["T"])
    target_time = parse_datetime(point["target_time_begin"])
    minute_of_day = forecast_time.hour * 60 + forecast_time.minute
    target_minute_of_day = target_time.hour * 60 + target_time.minute

    visible = [
        (parse_datetime(row["event_time"]), row)
        for row in telemetry
        if parse_datetime(row["event_time"]) <= forecast_time
    ]
    visible.sort(key=lambda item: item[0])
    window_5m = forecast_time - timedelta(minutes=5)
    window_10m = forecast_time - timedelta(minutes=10)
    recent_5m = [row for timestamp, row in visible if timestamp >= window_5m]
    recent_10m = [row for timestamp, row in visible if timestamp >= window_10m]

    def speeds(rows: Sequence[Mapping[str, Any]]) -> np.ndarray:
        return np.asarray(
            [
                float(row["speed"])
                for row in rows
                if row.get("speed") is not None
                and np.isfinite(float(row["speed"]))
            ],
            dtype=float,
        )

    speed_5m = speeds(recent_5m)
    speed_10m = speeds(recent_10m)
    last_timestamp, last_row = visible[-1] if visible else (None, {})
    heading = last_row.get("heading")
    heading = float(heading) if heading is not None and np.isfinite(float(heading)) else 0.0
    valid_location = bool(last_row.get("location_valid", False))
    lon = last_row.get("lon") if valid_location else None
    lat = last_row.get("lat") if valid_location else None
    alt = last_row.get("alt")

    def finite_or_zero(value: Any) -> float:
        if value is None:
            return 0.0
        result = float(value)
        return result if np.isfinite(result) else 0.0

    last_speed = finite_or_zero(last_row.get("speed"))
    mean_5m = float(speed_5m.mean()) if speed_5m.size else 0.0
    mean_10m = float(speed_10m.mean()) if speed_10m.size else 0.0
    return {
        "cur_dev_s": finite_or_zero(point.get("cur_dev_s")),
        "minutes_to_target": (target_time - forecast_time).total_seconds() / 60.0,
        "time_of_day_sin": sin(2.0 * np.pi * minute_of_day / 1440.0),
        "time_of_day_cos": cos(2.0 * np.pi * minute_of_day / 1440.0),
        "target_time_sin": sin(2.0 * np.pi * target_minute_of_day / 1440.0),
        "target_time_cos": cos(2.0 * np.pi * target_minute_of_day / 1440.0),
        "telemetry_age_s": (
            (forecast_time - last_timestamp).total_seconds() if last_timestamp else -1.0
        ),
        "telemetry_count_5m": float(len(recent_5m)),
        "telemetry_count_10m": float(len(recent_10m)),
        "location_valid": float(valid_location),
        "speed_last": last_speed,
        "speed_mean_5m": mean_5m,
        "speed_std_5m": float(speed_5m.std()) if speed_5m.size else 0.0,
        "speed_mean_10m": mean_10m,
        "speed_trend_5m": last_speed - mean_5m if speed_5m.size else 0.0,
        "heading_sin": sin(radians(heading)),
        "heading_cos": cos(radians(heading)),
        "lon": finite_or_zero(lon),
        "lat": finite_or_zero(lat),
        "alt": finite_or_zero(alt),
    }


def make_training_features(points: Any, traffic: Any) -> Any:
    """Create leakage-safe features for dataframe inputs."""
    traffic_by_vehicle: dict[int, list[dict[str, Any]]] = {}
    for row in traffic.to_dict(orient="records"):
        row["event_time"] = parse_datetime(row["event_time"])
        traffic_by_vehicle.setdefault(int(row["tr_id"]), []).append(row)
    for rows in traffic_by_vehicle.values():
        rows.sort(key=lambda row: row["event_time"])

    features: list[dict[str, float]] = []
    for point in points.to_dict(orient="records"):
        vehicle_rows = traffic_by_vehicle.get(int(point["tr_id"]), [])
        features.append(extract_features(point, vehicle_rows))
    return points.__class__(features, index=points.index)[FEATURE_NAMES]


def make_training_sequences(
    points: Any,
    traffic: Any,
    sequence_length: int = DEFAULT_SEQUENCE_LENGTH,
) -> np.ndarray:
    """Build fixed-length, chronological telemetry sequences available at each T."""
    if sequence_length < 1:
        raise ValueError("sequence_length must be at least 1")

    traffic_by_vehicle: dict[int, list[dict[str, Any]]] = {}
    times_by_vehicle: dict[int, list[datetime]] = {}
    for row in traffic.to_dict(orient="records"):
        row["event_time"] = parse_datetime(row["event_time"])
        vehicle_id = int(row["tr_id"])
        traffic_by_vehicle.setdefault(vehicle_id, []).append(row)
    for vehicle_id, rows in traffic_by_vehicle.items():
        rows.sort(key=lambda row: row["event_time"])
        times_by_vehicle[vehicle_id] = [row["event_time"] for row in rows]

    sequences = np.zeros(
        (len(points), sequence_length, len(SEQUENCE_FEATURE_NAMES)),
        dtype=np.float32,
    )
    point_rows = points.to_dict(orient="records")
    for point_index, point in enumerate(point_rows):
        forecast_time = parse_datetime(point["T"])
        vehicle_id = int(point["tr_id"])
        rows = traffic_by_vehicle.get(vehicle_id, [])
        event_times = times_by_vehicle.get(vehicle_id, [])
        end = bisect_right(event_times, forecast_time)
        visible_rows = rows[max(0, end - sequence_length) : end]
        offset = sequence_length - len(visible_rows)

        for sequence_index, row in enumerate(visible_rows, start=offset):
            age_s = (forecast_time - row["event_time"]).total_seconds()
            speed = row.get("speed")
            heading = row.get("heading")
            valid = bool(row.get("location_valid", False))
            lon = row.get("lon") if valid else None
            lat = row.get("lat") if valid else None

            def finite(value: Any) -> float:
                if value is None:
                    return 0.0
                result = float(value)
                return result if np.isfinite(result) else 0.0

            speed_value = finite(speed)
            heading_value = finite(heading)
            sequences[point_index, sequence_index] = (
                max(-1.0, min(0.0, -age_s / 600.0)),
                max(0.0, min(2.0, speed_value / 40.0)),
                sin(radians(heading_value)),
                cos(radians(heading_value)),
                max(-5.0, min(5.0, (finite(lon) - 37.6) / 0.1)),
                max(-5.0, min(5.0, (finite(lat) - 55.75) / 0.1)),
                max(-2.0, min(2.0, finite(row.get("alt")) / 1000.0)),
                float(valid),
            )
    return sequences
