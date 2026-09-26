from datetime import datetime
import unittest

from delay_service.features import extract_features, make_training_sequences
import pandas as pd


class FeatureExtractionTests(unittest.TestCase):
    def test_future_telemetry_is_ignored(self) -> None:
        point = {
            "T": datetime.fromisoformat("2026-01-06T03:35:00"),
            "target_time_begin": datetime.fromisoformat("2026-01-06T03:50:00"),
            "cur_dev_s": 80.0,
        }
        telemetry = [
            {
                "event_time": "2026-01-06T03:34:00",
                "speed": 20.0,
                "heading": 90.0,
                "location_valid": True,
                "lon": 37.5,
                "lat": 55.7,
            },
            {
                "event_time": "2026-01-06T03:36:00",
                "speed": 100.0,
                "heading": 180.0,
                "location_valid": True,
                "lon": 40.0,
                "lat": 60.0,
            },
        ]

        features = extract_features(point, telemetry)

        self.assertEqual(features["speed_last"], 20.0)
        self.assertEqual(features["telemetry_count_5m"], 1.0)
        self.assertEqual(features["telemetry_age_s"], 60.0)
        self.assertAlmostEqual(features["lon"], 37.5)

    def test_no_telemetry_uses_finite_neutral_features(self) -> None:
        point = {
            "T": datetime.fromisoformat("2026-01-06T03:35:00"),
            "target_time_begin": datetime.fromisoformat("2026-01-06T03:50:00"),
            "cur_dev_s": -10.0,
        }

        features = extract_features(point, [])

        self.assertEqual(features["cur_dev_s"], -10.0)
        self.assertEqual(features["telemetry_age_s"], -1.0)
        self.assertTrue(all(value == value for value in features.values()))

    def test_sequence_contains_only_chronological_telemetry_up_to_t(self) -> None:
        points = pd.DataFrame(
            [
                {
                    "tr_id": 1,
                    "T": datetime.fromisoformat("2026-01-06T03:35:00"),
                }
            ]
        )
        traffic = pd.DataFrame(
            [
                {
                    "tr_id": 1,
                    "event_time": "2026-01-06T03:34:00",
                    "location_valid": True,
                    "speed": 12.0,
                    "heading": 0.0,
                    "lon": 37.5,
                    "lat": 55.7,
                    "alt": 100.0,
                },
                {
                    "tr_id": 1,
                    "event_time": "2026-01-06T03:36:00",
                    "location_valid": True,
                    "speed": 100.0,
                    "heading": 180.0,
                    "lon": 40.0,
                    "lat": 60.0,
                    "alt": 500.0,
                },
            ]
        )

        sequences = make_training_sequences(points, traffic, sequence_length=3)

        self.assertEqual(sequences.shape, (1, 3, 8))
        self.assertEqual(sequences[0, 0].sum(), 0.0)
        self.assertAlmostEqual(float(sequences[0, -1, 1]), 12.0 / 40.0)


if __name__ == "__main__":
    unittest.main()
