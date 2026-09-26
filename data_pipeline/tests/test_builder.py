import json
import tempfile
import unittest
from pathlib import Path

from route_catalog.builder import (
    build_catalog,
    canonical_cycle,
    load_dataset,
    stable_pattern_id,
    stable_stop_id,
    write_artifacts,
)


DATASET = Path(__file__).resolve().parents[4] / "dataset"


class RouteCatalogTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bundle = load_dataset(DATASET)
        cls.catalog, cls.bindings, cls.quality = build_catalog(cls.bundle)

    def test_real_mapping_is_one_to_one_and_stable(self):
        real = self.bindings[~self.bindings.synthetic]
        self.assertEqual(len(real), 30)
        self.assertEqual(real.unit_id.nunique(), 30)
        self.assertEqual(real.tr_id.nunique(), 30)
        self.assertEqual(
            int(real.loc[real.unit_id.eq(786201), "tr_id"].iloc[0]),
            122658,
        )

    def test_test_and_validate_traffic_are_identical(self):
        self.assertTrue(self.quality["traffic_files_test_validate_identical"])

    def test_stable_ids_are_order_independent_for_cycle_rotation(self):
        sequence = ["a", "b", "c", "a"]
        self.assertEqual(canonical_cycle(sequence), ["a", "b", "c"])
        self.assertEqual(stable_pattern_id(sequence), stable_pattern_id(["b", "c", "a", "b"]))
        self.assertEqual(stable_stop_id(37.1, 55.7), stable_stop_id(37.10000001, 55.70000001))

    def test_multiple_route_patterns_are_detected_for_134040(self):
        patterns = {
            item["route_pattern_id"]
            for item in self.catalog["assignments"]
            if item["tr_id"] == 134040
        }
        self.assertGreaterEqual(len(patterns), 2)

    def test_repeated_rounds_are_split_into_single_occurrences(self):
        occurrences = [
            item for item in self.catalog["assignments"] if item["tr_id"] == 122658
        ]
        self.assertGreaterEqual(len(occurrences), 6)
        self.assertLessEqual(max(len(item["events"]) for item in occurrences), 40)
        action_ids = [event["action_item_id"] for item in occurrences for event in item["events"]]
        self.assertEqual(len(action_ids), len(set(action_ids)))

    def test_134494_does_not_claim_unrelated_gps_geometry(self):
        pattern_ids = {
            item["route_pattern_id"]
            for item in self.catalog["assignments"]
            if item["tr_id"] == 134494
        }
        qualities = {
            item["route_pattern_id"]: item["geometry_quality"]
            for item in self.catalog["route_patterns"]
        }
        self.assertTrue(pattern_ids)
        self.assertEqual({qualities[item] for item in pattern_ids}, {"stops_only"})

    def test_serialization_is_deterministic(self):
        with tempfile.TemporaryDirectory() as left, tempfile.TemporaryDirectory() as right:
            write_artifacts(self.catalog, self.bindings, self.quality, left)
            write_artifacts(self.catalog, self.bindings, self.quality, right)
            for name in ("vehicle_bindings.csv", "route_catalog.json", "route_quality_report.json"):
                self.assertEqual((Path(left) / name).read_bytes(), (Path(right) / name).read_bytes())
            json.loads((Path(left) / "route_catalog.json").read_text())


if __name__ == "__main__":
    unittest.main()
