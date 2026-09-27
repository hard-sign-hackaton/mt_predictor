# MT Predictor

Диспетчерская карта с прогнозом задержек и постоянным жизненным циклом инцидентов. Система принимает бинарный NDTP-поток, сопоставляет `unit_id` с вычисленным паттерном маршрута, считает задержку через ML по gRPC, определяет возможные причины инцидента и хранит историю с сообщениями диспетчера в PostgreSQL.

## Быстрый запуск

Требуются Docker Desktop / Docker Engine с Compose и исходный датасет в родительской папке репозитория (`../train`, `../validate`, `../labels`). Весь демонстрационный контур запускается одной командой:

```bash
cd mt_predictor
./demo/start-demo.sh
```

После запуска:

- карта: <http://localhost:5173>;
- health backend: <http://localhost:8080/health>;
- HTTP/SSE API: `localhost:8080`;
- TCP-приёмник NDTP: `localhost:9201`.
- gRPC ML: `localhost:50051`.

Остановить контур:

```bash
docker compose --profile official-emulator down
```

По умолчанию контейнер `feeder` читает реальные траектории из `dataset/validate/traffic.csv`, сохраняет исходный `event_time`, кодирует строки обратно в бинарные NDTP-пакеты и отправляет их в тот же TCP listener, который принимает официальный эмулятор. Историческое время требуется, чтобы demo-телеметрия совпадала с календарём расписания и могла честно сформировать ML-цель. Это не отдельный путь загрузки данных: декодирование, map matching и live-выдача у обоих источников общие.

## Что запускается

```text
traffic.csv -> NDTP feeder --TCP:9201--┐
                                      ├-> decode -> map matching -> cur_dev_s
официальный эмулятор --TCP:9201-------┘                         |
                                                                v
React <- HTTP + SSE <- predictions/incidents <- gRPC -> ML ONNX
```

- `backend` загружает вычисленный каталог маршрутов один раз при старте, слушает NDTP, асинхронно вызывает ML и публикует API карты и инцидентов;
- `ml` загружает CatBoost + Transformer и использует FP32 ONNX Runtime для Transformer-части на CPU;
- `feeder` воспроизводит восемь наиболее полных реальных треков с ускорением `5x`;
- `frontend` показывает географическую карту и отдельную вкладку активных инцидентов;
- `postgres` постоянно хранит текущие, ожидающие и завершённые инциденты;
- синтетические train-примеры не попадают в каталог live-карты.

Плитки OpenStreetMap загружаются из интернета. Без сети сохраняются маршруты, остановки и транспорт, но не базовая подложка.

## Инструкция запуска

Нужны Docker Desktop / Docker Engine с Compose. Команды выполняются из корня `mt_predictor`. Перед переключением режима остановите предыдущий источник телеметрии:

```bash
docker compose --profile mock --profile official-emulator down
```

### Сценарий 1. Данные из исходного датасета

Скрипт ожидает `../../dataset/validate/traffic.csv`; другой каталог задаётся через `DATASET_DIR`. Feeder выбирает восемь наиболее полных траекторий, сохраняет исходную временную шкалу, кодирует строки в бинарные NDTP-пакеты и воспроизводит их с ускорением 50x.

```bash
docker compose up -d --build postgres ml backend frontend feeder
```

Телеметрия проходит штатные decode, map matching, сопоставление с расписанием и реальный ML-прогноз. Расширенных полей дверей и дорожной обстановки в исходном CSV нет, поэтому диагностика использует только доступные признаки.

### Сценарий 2. Официальный эмулятор NDTP

Проверяет приём живых бинарных пакетов от образа организаторов. Образ лежит вне репозитория и сначала загружается вручную:

```bash
docker load -i ../../dataset/ndtp-telemetry-emulator.tar
```

Запуск контура одной командой, как в первом сценарии, но вместо `feeder` поднимается `official-emulator`:

```bash
docker compose up -d --build postgres ml backend frontend official-emulator
```

Профиль `official-emulator` включается автоматически, потому что сервис назван по имени. Затем эмулятору передаётся конфигурация тестовых ТС:

```bash
curl -X POST http://localhost:18080/api/config -H "Content-Type: application/json" --data-binary @demo/official-emulator-config.json
```

### Сценарий 3. Mock-данные с обогащённой телеметрией

Демонстрационный режим для ML-инцидентов и вычисления причин: пять движущихся фрагментов реальных маршрутов и синхронизированные признаки дверей, скорости, стоянки, загруженности, качества GPS и отклонения от маршрута. Датасет не нужен.

```bash
./demo/start-demo.sh mock
```

Эквивалентный ручной запуск:

```bash
docker compose --profile mock up -d --build postgres ml backend frontend mock-feeder
curl -X POST http://localhost:8080/api/v1/demo/scenarios -H "Content-Type: application/json" --data-binary @backend/data/mock_scenarios.json
```

`mock_telemetry.csv` отвечает за движение пяти ТС, `mock_scenarios.json` — за синхронизированные телематические признаки и расписания. Задержку прогнозирует реальный ML-сервис; причина и числовые подтверждения вычисляются backend-правилами из телеметрии, а не подставляются frontend. Набор демонстрирует длительное открытие дверей, дорожное замедление, отклонение от маршрута, длительную стоянку и отставание от расписания.

### Проверка без браузера

```bash
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/dashboard/snapshot
```

## Настройка параметров

### Флаги backend

| Флаг | По умолчанию | Назначение |
|---|---|---|
| `-ndtp-addr` | `:9201` | TCP-адрес приёмника NDTP |
| `-http-addr` | `:8080` | HTTP-адрес API карты |
| `-catalog` | `data/generated/route_catalog.json` | Каталог маршрутов, читается один раз при старте |
| `-ml-addr` | `ml:50051` | Адрес gRPC ML; пустое значение отключает прогнозы |
| `-ml-timeout` | `3s` | Таймаут одного `PredictBatch` |
| `-prediction-interval` | `5m` | Минимальный шаг прогнозов по `event_time` одного ТС |
| `-incident-delay-threshold` | `120` | Порог создания инцидента, секунды |
| `-telemetry-ttl` | `30s` | Время до перехода ТС в состояние stale |
| `-enable-mock-scenarios` | `false` | Загрузка контролируемых demo-сценариев |

### Флаги feeder

| Флаг | По умолчанию | Назначение |
|---|---|---|
| `-addr` | `127.0.0.1:9201` | Адрес приёмника NDTP |
| `-file` | — | Путь к `traffic.csv`, обязателен |
| `-speed` | `60` | Множитель скорости; `0` отправляет как можно быстрее |
| `-vehicles` | `11` | Сколько ТС воспроизводить; `0` — все с реальной траекторией |
| `-limit` | `0` | Ограничение точек на ТС; `0` — без ограничения |
| `-loop` | `false` | Перезапуск прогона по его завершении |
| `-timestamp-mode` | `rebased` | Время на устройстве: `rebased` на текущее или `source` из CSV |
| `-v` | `false` | Лог каждой точки |

### Значения, заданные в `compose.yaml`

| Сервис | Параметры запуска |
|---|---|
| `feeder` | 8 ТС, `speed=50`, `timestamp-mode=source`, данные `../../dataset/validate/traffic.csv` |
| `mock-feeder` | 5 ТС, `speed=5`, `loop`, `timestamp-mode=rebased` |
| `backend` | `-enable-mock-scenarios`, `-ml-timeout 15s` |
| `ml` | `TRANSFORMER_BACKEND=onnx`, `MODEL_DEVICE=cpu` |

### Профили Compose

| Профиль | Что добавляет |
|---|---|
| `mock` | Сервис `mock-feeder` для сценария 3 |
| `official-emulator` | Сервис `official-emulator` с образом организаторов |

Остановка контура с очисткой источников телеметрии:

```bash
docker compose --profile mock --profile official-emulator down
```

Удаление также данных PostgreSQL:

```bash
docker compose --profile mock --profile official-emulator down -v
```

## Ссылки на сервисы

| Сервис | Адрес | Назначение |
|---|---|---|
| Карта (frontend) | <http://localhost:5173> | Географическая карта и вкладка активных инцидентов |
| Backend HTTP | <http://localhost:8080> | API карты, прогнозов и инцидентов; SSE live-поток |
| Health backend | <http://localhost:8080/health> | Версия каталога и счётчики ТС, прогнозов, инцидентов |
| Backend NDTP | `localhost:9201` | TCP listener бинарного протокола телематики |
| ML gRPC | `localhost:50051` | Сервис прогноза задержки |
| PostgreSQL | `postgres:5432` | Хранение инцидентов и действий оператора, внутри сети Compose |
| Официальный эмулятор | <http://localhost:18080/api/config> | Управление тестовыми ТС эмулятора |

### Ресурсы HTTP API

| Ресурс | Назначение |
|---|---|
| `GET /api/v1/map/init` | Статические остановки, геометрии route pattern, рейсы и реальные привязки `unit_id -> tr_id` |
| `GET /api/v1/map/snapshot` | Последнее согласованное состояние всех принятых ТС |
| `GET /api/v1/dashboard/snapshot` | Согласованный снимок ТС, последних прогнозов и активных инцидентов |
| `GET /api/v1/map/events` | SSE: полный `snapshot`, затем `vehicle_updated`, `prediction_updated`, `incident_updated` |
| `GET /api/v1/incidents/history` | История с фильтром результата и серверной пагинацией |
| `GET /api/v1/incidents/{id}` | Карточка инцидента любого состояния |
| `GET /api/v1/incidents/{id}/actions` | Допустимые реакции и история сообщений диспетчера |
| `POST /api/v1/incidents/{id}/actions` | Поставить сообщение диспетчера в очередь отправки |
| `POST /api/v1/demo/scenarios` | Загрузка mock-сценариев диагностических причин |

### Данные и документация

| Путь | Назначение |
|---|---|
| `backend/data/generated/route_catalog.json` | Вычисленный каталог маршрутов, читается backend при старте |
| `backend/data/generated/public_transport_reference.json` | Исходный публичный справочник и дата снимка |
| `backend/data/mock_telemetry.csv` | Движение пяти ТС для mock-сценария |
| `backend/data/mock_scenarios.json` | Признаки и расписания mock-сценария |
| `demo/official-emulator-config.json` | Конфигурация пяти тестовых ТС официального эмулятора |
| `data_pipeline/` | Построение каталога из `../train`, `../validate`, `../labels` |
| `docs/Backend_docs.md` | Подробное описание backend |
