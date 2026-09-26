from __future__ import annotations

import hashlib
import json
import math
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable

import numpy as np
import pandas as pd


EARTH_METERS_PER_DEGREE = 111_200.0
MOSCOW_LON_SCALE = 62_400.0
BLOCK_GAP_MINUTES = 45.0
MIN_PATTERN_STOPS = 8
GPS_COVERAGE_DISTANCE_METERS = 150.0
GPS_MIN_COVERAGE = 0.60
GPS_MIN_POINTS = 20


@dataclass
class DatasetBundle:
    traffic: dict[str, pd.DataFrame]
    schedules: dict[str, pd.DataFrame]
    prediction_points: dict[str, pd.DataFrame]
    source_hashes: dict[str, str]


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_dataset(dataset_dir: str | Path) -> DatasetBundle:
    root = Path(dataset_dir)
    files = {
        "train_traffic": root / "train" / "traffic.csv",
        "test_traffic": root / "test" / "traffic.csv",
        "validate_traffic": root / "validate" / "traffic.csv",
        "train_schedule": root / "train" / "schedule.csv",
        "test_schedule": root / "test" / "schedule.csv",
        "validate_schedule": root / "validate" / "schedule_plan.csv",
        "train_labels": root / "labels" / "labels_train.csv",
        "test_labels": root / "labels" / "labels_test.csv",
        "validate_points": root / "validate" / "points.csv",
    }
    missing = [str(path) for path in files.values() if not path.exists()]
    if missing:
        raise FileNotFoundError(f"dataset files are missing: {missing}")

    traffic: dict[str, pd.DataFrame] = {}
    for split in ("train", "test", "validate"):
        frame = pd.read_csv(files[f"{split}_traffic"], dtype={"packet_id": str})
        for column in ("event_time", "gps_time", "receive_time"):
            frame[column] = pd.to_datetime(frame[column], errors="coerce")
        traffic[split] = frame

    schedules: dict[str, pd.DataFrame] = {}
    for split in ("train", "test", "validate"):
        frame = pd.read_csv(files[f"{split}_schedule"])
        frame["time_begin"] = pd.to_datetime(frame["time_begin"], errors="raise")
        if "time_fact_begin" in frame:
            frame["time_fact_begin"] = pd.to_datetime(frame["time_fact_begin"], errors="coerce")
        coords = frame["geom"].str.extract(r"POINT \(([-0-9.]+) ([-0-9.]+)\)").astype(float)
        if coords.isna().any().any():
            raise ValueError(f"invalid WKT POINT in {split} schedule")
        frame["lon"] = coords[0]
        frame["lat"] = coords[1]
        frame["stop_id"] = [stable_stop_id(lon, lat) for lon, lat in zip(frame.lon, frame.lat)]
        schedules[split] = frame

    points = {
        "train": pd.read_csv(files["train_labels"]),
        "test": pd.read_csv(files["test_labels"]),
        "validate": pd.read_csv(files["validate_points"]),
    }
    return DatasetBundle(
        traffic=traffic,
        schedules=schedules,
        prediction_points=points,
        source_hashes={name: _sha256(path) for name, path in files.items()},
    )


def stable_stop_id(lon: float, lat: float) -> str:
    """Keep opposite platforms separate while merging exact recurring schedule points."""
    token = f"{lon:.6f},{lat:.6f}".encode()
    return "stop_" + hashlib.sha256(token).hexdigest()[:12]


def stable_pattern_id(stop_ids: Iterable[str]) -> str:
    canonical = canonical_cycle(list(stop_ids))
    return "route_pattern_" + hashlib.sha256("|".join(canonical).encode()).hexdigest()[:12]


def canonical_cycle(sequence: list[str]) -> list[str]:
    values = collapse_consecutive(sequence)
    if len(values) > 1 and values[0] == values[-1]:
        values = values[:-1]
    if not values:
        return []
    rotations = [values[index:] + values[:index] for index in range(len(values))]
    return min(rotations)


def collapse_consecutive(values: Iterable[str]) -> list[str]:
    result: list[str] = []
    for value in values:
        if not result or result[-1] != value:
            result.append(value)
    return result


def _distance_m(a_lon: float, a_lat: float, b_lon: float, b_lat: float) -> float:
    dx = (a_lon - b_lon) * EARTH_METERS_PER_DEGREE * math.cos(math.radians((a_lat + b_lat) / 2))
    dy = (a_lat - b_lat) * EARTH_METERS_PER_DEGREE
    return math.hypot(dx, dy)


def _nearest_distances(points: np.ndarray, reference: np.ndarray) -> np.ndarray:
    if len(points) == 0 or len(reference) == 0:
        return np.full(len(points), np.inf)
    result = np.full(len(points), np.inf)
    for start in range(0, len(points), 1000):
        chunk = points[start : start + 1000]
        dx = (chunk[:, None, 0] - reference[None, :, 0]) * MOSCOW_LON_SCALE
        dy = (chunk[:, None, 1] - reference[None, :, 1]) * EARTH_METERS_PER_DEGREE
        result[start : start + len(chunk)] = np.sqrt(dx * dx + dy * dy).min(axis=1)
    return result


def _distance_to_polyline(points: np.ndarray, line: np.ndarray) -> np.ndarray:
    if len(points) == 0 or len(line) < 2:
        return np.full(len(points), np.inf)
    px = points[:, 0] * MOSCOW_LON_SCALE
    py = points[:, 1] * EARTH_METERS_PER_DEGREE
    best = np.full(len(points), np.inf)
    for index in range(len(line) - 1):
        ax, ay = line[index, 0] * MOSCOW_LON_SCALE, line[index, 1] * EARTH_METERS_PER_DEGREE
        bx, by = line[index + 1, 0] * MOSCOW_LON_SCALE, line[index + 1, 1] * EARTH_METERS_PER_DEGREE
        dx, dy = bx - ax, by - ay
        denominator = dx * dx + dy * dy
        if denominator == 0:
            distance = np.hypot(px - ax, py - ay)
        else:
            progress = np.clip(((px - ax) * dx + (py - ay) * dy) / denominator, 0, 1)
            distance = np.hypot(px - (ax + progress * dx), py - (ay + progress * dy))
        best = np.minimum(best, distance)
    return best


def _split_schedule(schedule: pd.DataFrame) -> list[pd.DataFrame]:
    ordered = schedule.sort_values(["time_begin", "tt_action_item_id"]).reset_index(drop=True)
    gaps = ordered.time_begin.diff().dt.total_seconds().div(60).fillna(0)
    block = (gaps > BLOCK_GAP_MINUTES).cumsum()
    return [part.reset_index(drop=True) for _, part in ordered.groupby(block, sort=True)]


def _period_length(sequence: list[str]) -> int:
    sequence = collapse_consecutive(sequence)
    size = len(sequence)
    if size < MIN_PATTERN_STOPS * 2:
        return size
    candidates: list[tuple[float, int]] = []
    for period in range(MIN_PATTERN_STOPS, size // 2 + 1):
        comparisons = size - period
        if comparisons < MIN_PATTERN_STOPS:
            continue
        agreement = sum(sequence[i] == sequence[i + period] for i in range(comparisons)) / comparisons
        if agreement >= 0.82:
            candidates.append((agreement, period))
    if not candidates:
        return size
    best_agreement = max(score for score, _ in candidates)
    return min(period for score, period in candidates if score >= best_agreement - 0.03)


def _chunk_block(block: pd.DataFrame, fallback_period: int | None = None) -> list[pd.DataFrame]:
    # Repeated terminal rows are meaningful schedule events, but not useful when
    # discovering the period. Keep a mapping back to the original rows.
    keep = [0]
    for index in range(1, len(block)):
        if block.iloc[index].stop_id != block.iloc[index - 1].stop_id:
            keep.append(index)
    compact = block.iloc[keep].reset_index().rename(columns={"index": "source_index"})
    period = _period_length(compact.stop_id.tolist())
    if period == len(compact) and fallback_period and len(compact) >= int(fallback_period * 1.7):
        period = fallback_period
    if period == len(compact):
        return [block]
    chunks: list[pd.DataFrame] = []
    for start in range(0, len(compact), period):
        end = min(start + period, len(compact))
        if end - start < MIN_PATTERN_STOPS:
            if chunks:
                chunks[-1] = pd.concat([chunks[-1], block.iloc[compact.iloc[start].source_index :]])
            continue
        source_start = int(compact.iloc[start].source_index)
        source_end = int(compact.iloc[end].source_index) if end < len(compact) else len(block)
        chunks.append(block.iloc[source_start:source_end].copy())
    return chunks or [block]


def _cluster_chunks(chunks: list[pd.DataFrame]) -> list[tuple[str, pd.DataFrame]]:
    """Merge partial duties of the same line while keeping real route switches separate."""
    prepared = []
    for chunk in chunks:
        sequence = collapse_consecutive(chunk.stop_id.tolist())
        prepared.append((chunk, sequence, set(sequence)))
    prepared.sort(key=lambda item: (-len(item[1]), item[0].time_begin.min()))
    clusters: list[dict[str, Any]] = []
    assigned: dict[int, str] = {}
    for chunk, sequence, stop_set in prepared:
        best_index, best_similarity = -1, 0.0
        for index, cluster in enumerate(clusters):
            union = stop_set | cluster["stop_set"]
            similarity = len(stop_set & cluster["stop_set"]) / len(union) if union else 0.0
            if similarity > best_similarity:
                best_index, best_similarity = index, similarity
        if best_index >= 0 and best_similarity >= 0.65:
            cluster = clusters[best_index]
            cluster["members"].append(chunk)
            if len(sequence) > len(cluster["representative"]):
                cluster["representative"] = sequence
                cluster["stop_set"] = stop_set
        else:
            clusters.append({"members": [chunk], "representative": sequence, "stop_set": stop_set})
    for cluster in clusters:
        pattern_id = stable_pattern_id(cluster["representative"])
        for chunk in cluster["members"]:
            assigned[id(chunk)] = pattern_id
    return [(assigned[id(chunk)], chunk) for chunk in chunks]


def _clean_gps(points: pd.DataFrame) -> pd.DataFrame:
    ordered = points.sort_values(["event_time", "receive_time", "packet_id"]).copy()
    ordered = ordered.drop_duplicates(["event_time", "lon", "lat"], keep="last")
    accepted: list[int] = []
    previous = None
    for index, row in ordered.iterrows():
        if previous is not None:
            seconds = (row.event_time - previous.event_time).total_seconds()
            if seconds > 0:
                speed = _distance_m(previous.lon, previous.lat, row.lon, row.lat) / seconds
                if speed > 55.0:
                    continue
        accepted.append(index)
        previous = row
    return ordered.loc[accepted].reset_index(drop=True)


def _rdp(points: list[list[float]], epsilon_m: float = 20.0) -> list[list[float]]:
    if len(points) < 3:
        return points
    start, end = points[0], points[-1]
    ax, ay = start[0] * MOSCOW_LON_SCALE, start[1] * EARTH_METERS_PER_DEGREE
    bx, by = end[0] * MOSCOW_LON_SCALE, end[1] * EARTH_METERS_PER_DEGREE
    dx, dy = bx - ax, by - ay
    denominator = dx * dx + dy * dy
    best_distance, best_index = -1.0, -1
    for index, point in enumerate(points[1:-1], start=1):
        px, py = point[0] * MOSCOW_LON_SCALE, point[1] * EARTH_METERS_PER_DEGREE
        if denominator == 0:
            distance = math.hypot(px - ax, py - ay)
        else:
            t = max(0.0, min(1.0, ((px - ax) * dx + (py - ay) * dy) / denominator))
            distance = math.hypot(px - (ax + t * dx), py - (ay + t * dy))
        if distance > best_distance:
            best_distance, best_index = distance, index
    if best_distance <= epsilon_m:
        return [start, end]
    left = _rdp(points[: best_index + 1], epsilon_m)
    right = _rdp(points[best_index:], epsilon_m)
    return left[:-1] + right


def _iso(value: pd.Timestamp) -> str:
    timestamp = pd.Timestamp(value)
    if timestamp.tzinfo is None:
        timestamp = timestamp.tz_localize("UTC")
    return timestamp.isoformat().replace("+00:00", "Z")


def _event_rows(chunk: pd.DataFrame) -> list[dict[str, Any]]:
    return [
        {
            "action_item_id": int(row.tt_action_item_id),
            "stop_id": row.stop_id,
            "planned_at": _iso(row.time_begin),
            "lon": round(float(row.lon), 7),
            "lat": round(float(row.lat), 7),
        }
        for row in chunk.itertuples()
    ]


def _build_bindings(bundle: DatasetBundle) -> tuple[list[dict[str, Any]], dict[int, int]]:
    sources: dict[tuple[int, int], set[str]] = {}
    unit_to_tr: dict[int, int] = {}
    tr_to_unit: dict[int, int] = {}
    for split, frame in bundle.traffic.items():
        for row in frame[["unit_id", "tr_id"]].drop_duplicates().itertuples(index=False):
            unit_id, tr_id = int(row.unit_id), int(row.tr_id)
            if unit_id in unit_to_tr and unit_to_tr[unit_id] != tr_id:
                raise ValueError(f"unit_id {unit_id} maps to multiple tr_id values")
            if tr_id in tr_to_unit and tr_to_unit[tr_id] != unit_id:
                raise ValueError(f"tr_id {tr_id} maps to multiple unit_id values")
            unit_to_tr[unit_id] = tr_id
            tr_to_unit[tr_id] = unit_id
            sources.setdefault((unit_id, tr_id), set()).add(split)
    scheduled = set(bundle.schedules["train"].tr_id.astype(int))
    bindings = [
        {
            "unit_id": unit,
            "tr_id": tr,
            "source_splits": sorted(sources[(unit, tr)]),
            "has_schedule": tr in scheduled,
            "synthetic": tr >= 9_000_000,
        }
        for unit, tr in sorted(unit_to_tr.items())
    ]
    return bindings, unit_to_tr


def _build_stops(schedule: pd.DataFrame) -> list[dict[str, Any]]:
    result = []
    for stop_id, group in schedule.groupby("stop_id", sort=True):
        addresses = sorted({str(value) for value in group.building_address.dropna() if str(value).strip()})
        row = group.iloc[0]
        result.append(
            {
                "stop_id": stop_id,
                "lon": round(float(row.lon), 7),
                "lat": round(float(row.lat), 7),
                "address": addresses[0] if addresses else "",
            }
        )
    return result


def _candidate_gps(traffic: pd.DataFrame, chunk: pd.DataFrame) -> tuple[pd.DataFrame, float, float, float]:
    time_column = "time_fact_begin" if "time_fact_begin" in chunk and chunk.time_fact_begin.notna().all() else "time_begin"
    start = chunk[time_column].min() - pd.Timedelta(minutes=2)
    end = chunk[time_column].max() + pd.Timedelta(minutes=2)
    points = traffic[
        traffic.location_valid.eq(True)
        & traffic.lon.notna()
        & traffic.lat.notna()
        & traffic.event_time.between(start, end)
    ]
    points = _clean_gps(points)
    stops = chunk[["lon", "lat"]].drop_duplicates().to_numpy()
    # A duty window may contain depot travel or telemetry belonging to another
    # line served by the same vehicle. Keep a generous one-kilometre corridor
    # around the ordered schedule stops so those excursions cannot become the
    # displayed route geometry.
    if len(points) and len(stops) >= 2:
        corridor_distance = _distance_to_polyline(points[["lon", "lat"]].to_numpy(), stops)
        points = points.loc[corridor_distance <= 1_000].reset_index(drop=True)
    gps = points[["lon", "lat"]].to_numpy()
    distances = _nearest_distances(stops, gps)
    coverage = float(np.mean(distances <= GPS_COVERAGE_DISTANCE_METERS)) if len(distances) else 0.0
    median_distance = float(np.median(distances)) if len(distances) else math.inf
    point_rows = list(points.itertuples())
    jumps = [
        _distance_m(first.lon, first.lat, second.lon, second.lat)
        for first, second in zip(point_rows, point_rows[1:])
    ]
    max_jump = max(jumps, default=0.0)
    return points, coverage, median_distance, max_jump


def build_catalog(bundle: DatasetBundle) -> tuple[dict[str, Any], pd.DataFrame, dict[str, Any]]:
    bindings, _ = _build_bindings(bundle)
    schedule = bundle.schedules["train"].copy()
    traffic = bundle.traffic["train"]
    stops = _build_stops(schedule)

    assignments: list[dict[str, Any]] = []
    chunk_frames: dict[str, list[tuple[int, pd.DataFrame]]] = {}
    for tr_id, tr_schedule in schedule.groupby("tr_id", sort=True):
        raw_chunks: list[pd.DataFrame] = []
        blocks = _split_schedule(tr_schedule)
        learned_periods = []
        for block in blocks:
            compact = collapse_consecutive(block.stop_id.tolist())
            period = _period_length(compact)
            if period < len(compact):
                learned_periods.append(period)
        fallback_period = int(round(float(np.median(learned_periods)))) if learned_periods else None
        for block in blocks:
            raw_chunks.extend(_chunk_block(block, fallback_period))
        occurrence = 0
        for pattern_id, chunk in _cluster_chunks(raw_chunks):
            stop_ids = collapse_consecutive(chunk.stop_id.tolist())
            if len(stop_ids) < 2:
                continue
            events = _event_rows(chunk)
            assignment = {
                "occurrence_id": f"occ_{int(tr_id)}_{occurrence:03d}",
                "tr_id": int(tr_id),
                "route_pattern_id": pattern_id,
                "valid_from": events[0]["planned_at"],
                "valid_to": events[-1]["planned_at"],
                "events": events,
            }
            assignments.append(assignment)
            chunk_frames.setdefault(pattern_id, []).append((int(tr_id), chunk.copy()))
            occurrence += 1

    patterns: list[dict[str, Any]] = []
    quality_rows: list[dict[str, Any]] = []
    for pattern_id, occurrences in sorted(chunk_frames.items()):
        representative = sorted(
            (canonical_cycle(chunk.stop_id.tolist()) for _, chunk in occurrences),
            key=lambda values: (-len(values), values),
        )[0]
        stop_lookup = {row["stop_id"]: row for row in stops}
        stop_polyline = [[stop_lookup[s]["lon"], stop_lookup[s]["lat"]] for s in representative]
        candidates: list[tuple[bool, float, int, float, float, pd.DataFrame, int]] = []
        good_occurrences: list[pd.DataFrame] = []
        for tr_id, chunk in occurrences:
            tr_traffic = traffic[traffic.tr_id.eq(tr_id)]
            points, coverage, median_distance, max_jump = _candidate_gps(tr_traffic, chunk)
            continuous = max_jump <= 2_000
            candidates.append((continuous, coverage, len(points), median_distance, max_jump, points, tr_id))
            if continuous and coverage >= GPS_MIN_COVERAGE and len(points) >= GPS_MIN_POINTS:
                good_occurrences.append(points)
        candidates.sort(key=lambda value: (value[0], value[1], value[2], -value[3]), reverse=True)
        continuous, best_coverage, _, median_distance, max_jump, best_points, _ = candidates[0]
        if continuous and best_coverage >= GPS_MIN_COVERAGE and len(best_points) >= GPS_MIN_POINTS:
            raw_line = best_points[["lon", "lat"]].round(7).values.tolist()
            polyline = _rdp(raw_line)
            geometry_quality = "gps_repeated" if len(good_occurrences) >= 2 else "gps_single"
        else:
            polyline = stop_polyline
            geometry_quality = "stops_only"

        bins: dict[tuple[int, int], set[int]] = {}
        centers: dict[tuple[int, int], list[tuple[float, float]]] = {}
        for occurrence_index, points in enumerate(good_occurrences):
            seen: set[tuple[int, int]] = set()
            for row in points.itertuples():
                key = (round(row.lon * MOSCOW_LON_SCALE / 25), round(row.lat * EARTH_METERS_PER_DEGREE / 25))
                centers.setdefault(key, []).append((float(row.lon), float(row.lat)))
                seen.add(key)
            for key in seen:
                bins.setdefault(key, set()).add(occurrence_index)
        frequent = []
        for key, visits in bins.items():
            if len(visits) < 2:
                continue
            values = centers[key]
            frequent.append(
                {
                    "lon": round(float(np.median([v[0] for v in values])), 7),
                    "lat": round(float(np.median([v[1] for v in values])), 7),
                    "occurrence_count": len(visits),
                }
            )
        frequent.sort(key=lambda row: (-row["occurrence_count"], row["lon"], row["lat"]))
        frequent = frequent[:500]
        pattern = {
            "route_pattern_id": pattern_id,
            "stop_ids": representative,
            "polyline": polyline,
            "frequent_points": frequent,
            "geometry_quality": geometry_quality,
            "quality": {
                "occurrence_count": len(occurrences),
                "good_gps_occurrence_count": len(good_occurrences),
                "best_stop_coverage": round(best_coverage, 4),
                "median_stop_to_gps_m": None if math.isinf(median_distance) else round(median_distance, 1),
                "max_gps_jump_m": round(max_jump, 1),
                "polyline_point_count": len(polyline),
            },
        }
        patterns.append(pattern)
        quality_rows.append({"route_pattern_id": pattern_id, **pattern["quality"], "geometry_quality": geometry_quality})

    catalog = {
        "schema_version": 1,
        "source_hashes": bundle.source_hashes,
        "vehicle_bindings": bindings,
        "stops": stops,
        "route_patterns": patterns,
        "assignments": sorted(assignments, key=lambda row: (row["tr_id"], row["valid_from"], row["occurrence_id"])),
    }
    quality = {
        "schema_version": 1,
        "traffic_files_test_validate_identical": bundle.source_hashes["test_traffic"]
        == bundle.source_hashes["validate_traffic"],
        "binding_count": len(bindings),
        "real_binding_count": sum(not item["synthetic"] for item in bindings),
        "real_with_schedule_count": sum(not item["synthetic"] and item["has_schedule"] for item in bindings),
        "real_without_schedule_count": sum(not item["synthetic"] and not item["has_schedule"] for item in bindings),
        "physical_stop_count": len(stops),
        "route_pattern_count": len(patterns),
        "assignment_count": len(assignments),
        "geometry_quality_counts": pd.Series([row["geometry_quality"] for row in quality_rows]).value_counts().to_dict(),
        "patterns": quality_rows,
    }
    binding_frame = pd.DataFrame(bindings)
    binding_frame["source_splits"] = binding_frame.source_splits.map("|".join)
    return catalog, binding_frame, quality


def write_artifacts(
    catalog: dict[str, Any], bindings: pd.DataFrame, quality: dict[str, Any], output_dir: str | Path
) -> None:
    output = Path(output_dir)
    output.mkdir(parents=True, exist_ok=True)
    bindings.to_csv(output / "vehicle_bindings.csv", index=False, lineterminator="\n")
    for name, value in (("route_catalog.json", catalog), ("route_quality_report.json", quality)):
        (output / name).write_text(
            json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )
