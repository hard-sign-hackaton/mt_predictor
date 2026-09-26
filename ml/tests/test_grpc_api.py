from concurrent import futures
from datetime import datetime, timezone
import os
import unittest

import grpc
from google.protobuf.timestamp_pb2 import Timestamp

from delay_service.grpc_server import DelayPredictionServicer
from delay_service.proto import delay_service_pb2 as pb
from delay_service.proto import delay_service_pb2_grpc as pb_grpc


def timestamp(value: str) -> Timestamp:
    result = Timestamp()
    result.FromDatetime(datetime.fromisoformat(value).replace(tzinfo=timezone.utc))
    return result


class StubPredictor:
    def predict_one(self, point, telemetry):
        return point["cur_dev_s"] - 10

    def predict_many(self, points, telemetry):
        return [point["cur_dev_s"] - 10 for point in points]


class GrpcApiTests(unittest.TestCase):
    def setUp(self) -> None:
        proxy_variables = {
            key: os.environ.pop(key)
            for key in (
                "HTTP_PROXY",
                "HTTPS_PROXY",
                "ALL_PROXY",
                "http_proxy",
                "https_proxy",
                "all_proxy",
            )
            if key in os.environ
        }
        self.addCleanup(os.environ.update, proxy_variables)
        self.server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
        pb_grpc.add_DelayPredictionServiceServicer_to_server(
            DelayPredictionServicer(StubPredictor()), self.server
        )
        port = self.server.add_insecure_port("127.0.0.1:0")
        self.server.start()
        self.addCleanup(self.server.stop, 0)
        self.channel = grpc.insecure_channel(f"127.0.0.1:{port}")
        grpc.channel_ready_future(self.channel).result(timeout=5)
        self.addCleanup(self.channel.close)
        self.stub = pb_grpc.DelayPredictionServiceStub(self.channel)

    @staticmethod
    def make_point(sample_id: str, cur_dev_s: float) -> pb.PredictionPoint:
        point = pb.PredictionPoint(
            sample_id=sample_id,
            tr_id=123,
            cur_dev_s=cur_dev_s,
        )
        point.T.CopyFrom(timestamp("2026-01-06T03:35:00"))
        point.target_time_begin.CopyFrom(timestamp("2026-01-06T03:50:00"))
        return point

    def test_predict_rpc_returns_prediction(self) -> None:
        request = pb.PredictRequest(point=self.make_point("bus_123", 274))
        telemetry = request.telemetry.add()
        telemetry.event_time.CopyFrom(timestamp("2026-01-06T03:34:50"))
        telemetry.location_valid = True
        telemetry.speed = 24

        response = self.stub.Predict(request, timeout=5)

        self.assertEqual(response.sample_id, "bus_123")
        self.assertEqual(response.prediction, 264)
        self.assertEqual(response.unit, "seconds")

    def test_predict_batch_rpc_returns_each_prediction(self) -> None:
        request = pb.BatchPredictRequest(
            points=[
                self.make_point("first", 20),
                self.make_point("second", -30),
            ]
        )

        response = self.stub.PredictBatch(request, timeout=5)

        self.assertEqual(
            [(item.sample_id, item.prediction) for item in response.predictions],
            [("first", 10), ("second", -40)],
        )

    def test_predict_rpc_rejects_missing_required_fields(self) -> None:
        with self.assertRaises(grpc.RpcError) as raised:
            self.stub.Predict(pb.PredictRequest(), timeout=5)

        self.assertEqual(raised.exception.code(), grpc.StatusCode.INVALID_ARGUMENT)


if __name__ == "__main__":
    unittest.main()
