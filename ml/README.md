# Predictor

Автономный gRPC-сервис для прогнозирования задержек транспорта на остановках. В репозитории находятся исходный код сервиса, сгенерированные привязки Protobuf/gRPC, тесты, манифест зависимостей и готовый дамп модели CatBoost + Transformer. Данные для обучения в репозиторий намеренно не включены.

Размер сохранённой модели — около 1,2 МБ. Она обучалась с CUDA, но её веса не привязаны к GPU и могут загружаться на CPU. Если CUDA доступна, инференс автоматически использует её. Поддержка Apple MPS пока не реализована, поэтому на Apple Silicon Transformer работает на CPU.

## Установка

Создайте виртуальное окружение из корня репозитория:

```powershell
py -m venv .venv
.\.venv\Scripts\python.exe -m pip install -r requirements.txt
```

В Linux или macOS:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
```

### Дополнительно: NVIDIA CUDA

В Windows с драйвером NVIDIA, совместимым с CUDA 12.8, замените версию PyTorch по умолчанию (для CPU) на сборку с CUDA:

```powershell
.\.venv\Scripts\python.exe -m pip install --force-reinstall --index-url https://download.pytorch.org/whl/cu128 "torch>=2.9,<3"
.\.venv\Scripts\python.exe -c "import torch; print(torch.cuda.is_available(), torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'CPU')"
```

gRPC-сервис работает и без GPU NVIDIA — в этом случае он использует CPU. Установите `MODEL_DEVICE=cuda`, чтобы потребовать CUDA, или `MODEL_DEVICE=cpu`, чтобы принудительно использовать CPU.

## Запуск gRPC-сервера

Из корня репозитория:

```powershell
.\.venv\Scripts\python.exe -m delay_service.grpc_server --host 0.0.0.0 --port 50051
```

В Linux или macOS:

```bash
.venv/bin/python -m delay_service.grpc_server --host 0.0.0.0 --port 50051
```

Дамп модели загружается из `delay_service/artifacts/delay_model.joblib`. Путь можно изменить с помощью `DELAY_MODEL_PATH`. Настройки сервера задаются переменными `GRPC_HOST`, `GRPC_PORT` и `GRPC_MAX_WORKERS`. Каждый процесс сервера при запуске загружает одну неизменяемую версию модели и затем не хранит состояние запросов, поэтому его можно запускать как реплику за балансировщиком нагрузки с поддержкой gRPC.

### Docker

Контекст сборки — каталог, в котором находится `requirements.txt`:

```bash
docker build -t mt-predictor-grpc .
docker run --rm -p 50051:50051 mt-predictor-grpc
```

В образ устанавливается версия PyTorch только для CPU. Контейнер запускается от непривилегированного пользователя и обслуживает включённую в репозиторий модель на порту 50051. Для сборки с поддержкой NVIDIA используйте:

```bash
docker build -t mt-predictor-grpc \
  --build-arg TORCH_INDEX_URL=https://download.pytorch.org/whl/cu128 \
  --build-arg TORCH_SUFFIX=+cu128 .
docker run --rm --gpus all -e MODEL_DEVICE=cuda -p 50051:50051 mt-predictor-grpc
```

## Оптимизация инференса с ONNX (необязательно)

По умолчанию используется стандартный путь PyTorch. Чтобы экспортировать часть обученной модели Transformer в формат ONNX с поддержкой динамического размера пакета, установите дополнительные инструменты и выполните экспорт рядом с моделью:

```powershell
.\.venv\Scripts\python.exe -m pip install -r requirements-onnx.txt
.\.venv\Scripts\python.exe -m delay_service.optimize --model-path delay_service\artifacts\delay_model.joblib
```

Экспортёр проверяет граф ONNX и сообщает, совпадают ли прогнозы с PyTorch. Чтобы при запуске сервера включить CPU-бэкенд ONNX Runtime:

```powershell
$env:TRANSFORMER_BACKEND = "onnx"
.\.venv\Scripts\python.exe -m delay_service.grpc_server
```

По умолчанию дополнительный файл ONNX ожидается рядом с файлом модели и имеет расширение `.onnx`. Путь можно изменить переменной `TRANSFORMER_ONNX_PATH`. Для снижения задержки одиночного запроса ONNX Runtime по умолчанию использует один внутрипоточный поток. Значения `ONNX_INTRA_OP_THREADS` и `ONNX_INTER_OP_THREADS` можно настроить с учётом размера пакета и характеристик CPU.

В Docker-образ можно включить ONNX Runtime с помощью `--build-arg INSTALL_ONNX_RUNTIME=1`. Соберите образ, включив уже экспортированный дополнительный файл модели в контекст сборки, и активируйте бэкенд при запуске:

```powershell
docker build -t mt-predictor-grpc --build-arg INSTALL_ONNX_RUNTIME=1 .
docker run --rm -p 50051:50051 -e TRANSFORMER_BACKEND=onnx mt-predictor-grpc
```

Вместо этого можно подключить соответствующий дополнительный файл `.onnx` рядом с моделью и задать `TRANSFORMER_ONNX_PATH`. Также доступен динамический экспорт INT8 с параметром `--quantize-int8`; в результате создаётся дополнительный файл `.int8.onnx`. INT8 — экспериментальный режим: прежде чем использовать его, сравните MAE и ошибки по отдельным строкам на отложенной размеченной выборке. По умолчанию этот режим не включён: локальные измерения показали незначительный выигрыш по задержке для небольших пакетов.

TensorRT пока не подключён к сервису. Для него нужна совместимая среда TensorRT в NVIDIA/Linux и провайдер ONNX Runtime с поддержкой TensorRT. Текущий бэкенд ONNX намеренно выбирает только `CPUExecutionProvider`.

Локальный микробенчмарк (это рабочее окружение, синтетические запросы с 32 записями телеметрии; измерено только время предиктора, без gRPC и сети), медианная задержка:

| Бэкенд | 1 прогноз | 16 прогнозов | 64 прогноза |
|---|---:|---:|---:|
| PyTorch CPU | 8.08 мс | 17.62 мс | 43.38 мс |
| ONNX Runtime FP32 CPU | 6.91 мс | 16.30 мс | 44.84 мс |
| ONNX Runtime INT8 CPU | 6.86 мс | 16.26 мс | 44.83 мс |

На размеченной тестовой выборке MAE FP32 ONNX совпала с MAE PyTorch CPU (53.2738 с). MAE INT8 составила 53.3436 с, но максимальное изменение прогноза для отдельной строки достигло 15.05 с. Эти результаты позволяют рекомендовать FP32 ONNX для одиночных прогнозов с низкой задержкой на этом CPU, а для крупных пакетов — PyTorch, если только бенчмарки на целевом оборудовании не покажут обратное. Это локальные измерения, а не гарантия уровня обслуживания (SLA).

Контракт сервиса Protobuf: `delay_service.v1.DelayPredictionService`:

- `Predict(PredictRequest) returns (PredictionResponse)`
- `PredictBatch(BatchPredictRequest) returns (BatchPredictionResponse)`

Контракт описан в [`delay_service/proto/delay_service.proto`](delay_service/proto/delay_service.proto). Передавайте записи телеметрии вместе с их `tr_id`. Записи, время которых позже времени прогноза `T`, игнорируются.

## Пример клиента

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

При подключении через недоверенную сеть используйте учётные данные TLS.

## Дообучение и публикация версии модели (необязательно)

Для обучения нужна исходная структура набора данных (`train/`, `test/`, `validate/` и `labels/`) рядом с этим репозиторием либо укажите расположение с помощью `--data-dir`. Обученная ансамблевая модель и файл `submission.csv` с результатами проверки записываются по путям, заданным командой. Если параметр `--model-out` не указан, обученная версия публикуется в файл с временной меткой в `delay_service/artifacts/versions/`. Для обучения используется размеченная обучающая выборка; метки тестовой выборки применяются только для оценки. Чтобы дообучить модель на новых метках, соберите обновлённый накопительный набор данных с новой тестовой выборкой, в которой транспортные средства не пересекаются с обучающей, а затем обучите и оцените новую версию:

```powershell
.\.venv\Scripts\python.exe -m delay_service.train --data-dir "C:\path\to\dataset" --device auto
```

Задайте `--device cuda`, чтобы потребовать NVIDIA CUDA, или `--device cpu`, чтобы принудительно использовать CPU. Чтобы выбрать явный путь неизменяемой версии, укажите `--model-out`, например:

```powershell
.\.venv\Scripts\python.exe -m delay_service.train --data-dir "C:\path\to\dataset" --model-out "C:\models\delay_model-v2.joblib"
```

При публикации артефакт записывается во временный файл и заменяется атомарно, поэтому неудачная запись не оставит по опубликованному пути повреждённую модель. Работающий сервер не загружает новые артефакты автоматически: проверьте новую версию, затем разверните её, перезапустив или заменив реплики с `DELAY_MODEL_PATH`, указывающим на эту версию. Сохраните предыдущий артефакт для отката. Обучение — это полное офлайн-переобучение на накопленных размеченных данных, а не обучение в реальном времени на запросах прогноза.

## Горизонтальное масштабирование

Описанный выше Docker-образ содержит встроенную модель. Чтобы развернуть новую версию, подключите артефакт к каждой реплике и задайте `DELAY_MODEL_PATH`. Запустите несколько реплик за внешним gRPC-балансировщиком нагрузки; выделите каждой реплике достаточно CPU и RAM и не перегружайте общую GPU. `PredictBatch` поддерживает до 1 000 точек прогноза в одном запросе, а `GRPC_MAX_WORKERS` задаёт размер пула обработчиков запросов на процесс. Подбирайте число реплик и обработчиков тестированием на целевом оборудовании: простое увеличение числа потоков не гарантирует более высокой пропускной способности.

## Тесты

Из корня репозитория:

```powershell
.\.venv\Scripts\python.exe -m unittest discover -s tests -v
```

## Файлы

- `delay_service/` — сервис, код модели, контракт и привязки Protobuf, а также модель.
- `tests/` — тесты признаков, модели и локального gRPC-сервера в процессе.
- `requirements.txt` — зависимости сервиса и обучения.
- `requirements-onnx.txt` — дополнительные средства экспорта ONNX и CPU-среда выполнения.
- `Dockerfile`, `.dockerignore` — файлы контейнерного образа gRPC-сервиса.
- `.gitignore` — исключает локальные окружения, кэш и копии файлов наборов данных.
