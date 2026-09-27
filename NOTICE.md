# NOTICE

Настоящий документ содержит сводку сторонних компонентов, используемых в проекте MT Predictor, а также их типовые лицензии. Он предназначен для прозрачности и удобства проверки соответствия требованиям OSS-совместимости при распространении, сборке или запуске проекта.

Примечание: это не юридическая консультация и не замена официальным текстам лицензий upstream-проектов. Для конечного распространения и готового дистрибутива следует сохранять оригинальные тексты лицензий и проверять точную версию каждого пакета в момент сборки.

## 1. Общее описание

Проект использует компоненты из следующих экосистем:

- Go backend: стандартный набор библиотек Go и сторонние зависимости из `backend/go.mod`
- Python ML-сервис: зависимости из `ml/requirements.txt` и `ml/requirements-onnx.txt`
- Frontend: зависимости из `frontend/package.json`
- Контейнеры: официальные образы Docker (`postgres:17-alpine`, `python:3.12-slim-bookworm`, `node:22-alpine`, `golang:1.26-bookworm`)

## 2. Лицензии и применимые стандарты

Для проекта применяются общепринятые открытые лицензии и стандарты OSS-кода, в первую очередь:

- MIT License
- Apache License 2.0
- BSD License (2-Clause / 3-Clause)
- ISC License
- PostgreSQL License (для PostgreSQL)
- Python Software Foundation License (для Python runtime и некоторые пакеты)
- MPL 2.0 (если используется в транзитивных зависимостях и конкретно указано upstream-метаданными)
- Go BSD-3-Clause/Apache-2.0-compatible licensing model for stdlib and module ecosystem

## 3. Go backend

Файл: `backend/go.mod`

Основные зависимости и их типовые лицензии:

- `github.com/google/uuid` — BSD-3-Clause
- `github.com/jackc/pgx/v5` — MIT
- `github.com/stretchr/testify` — MIT
- `google.golang.org/grpc` — Apache-2.0
- `google.golang.org/protobuf` — BSD-3-Clause
- `golang.org/x/crypto` — BSD-3-Clause
- `golang.org/x/net` — BSD-3-Clause
- `golang.org/x/sync` — BSD-3-Clause
- `golang.org/x/sys` — BSD-3-Clause
- `golang.org/x/text` — BSD-3-Clause

Дополнительно используемый Go toolchain и stdlib идут вместе с Go License и общепринятыми правилами лицензирования Go ecosystem.

## 4. Python ML-сервис

Файлы: `ml/requirements.txt`, `ml/requirements-onnx.txt`

Основные зависимости и их типовые лицензии:

- `catboost` — Apache-2.0
- `grpcio` — Apache-2.0
- `grpcio-tools` — Apache-2.0
- `joblib` — BSD-3-Clause
- `numpy` — BSD-3-Clause
- `pandas` — BSD-3-Clause
- `protobuf` — BSD-3-Clause
- `pydantic` — MIT
- `scikit-learn` — BSD-3-Clause
- `torch` — BSD-3-Clause
- `onnx` — MIT
- `onnxruntime` — MIT

Примечание: для отдельных пакетов можно использовать иной набор привязанных лицензий в зависимости от сборки и платформы; при production release обязательно хранить upstream license text вместе с собранным артефактом.

## 5. Frontend

Файлы: `frontend/package.json`, `frontend/package-lock.json`

Основные зависимости и типовые лицензии:

- `react` — MIT
- `react-dom` — MIT
- `react-router-dom` — MIT
- `leaflet` — BSD-2-Clause
- `react-leaflet` 5.0.0 — Hippocratic-2.1 (согласно lock-файлу)
- `@react-leaflet/core` 3.0.0 — Hippocratic-2.1 (согласно lock-файлу)
- `vite` — MIT
- `typescript` — Apache-2.0
- `@vitejs/plugin-react` — MIT
- `@types/react`, `@types/react-dom`, `@types/node` — MIT
- `eslint` — MIT
- `vitest` — MIT
- `@testing-library/*` — MIT
- `jsdom` — MIT

### OpenStreetMap и картографические данные

Карта в `frontend/src/components/LiveMap.tsx` отображает тайлы стандартного сервера `tile.openstreetmap.org`. Leaflet — это библиотека отображения карты, а OpenStreetMap (OSM) — отдельный проект открытых картографических данных; лицензия Leaflet не заменяет условия использования данных или тайлов OSM.

- Данные OpenStreetMap доступны на условиях Open Data Commons Open Database License (ODbL) 1.0: https://opendatacommons.org/licenses/odbl/1-0/
- Для используемого стандартного сервера тайлов действуют отдельные правила использования tile.openstreetmap.org: https://operations.osmfoundation.org/policies/tiles/. Эти правила не являются лицензией на Leaflet или самостоятельной лицензией на данные; они регулируют доступ к общедоступному серверу тайлов, включая ограничения использования и требования к клиентам.
- Обязательная атрибуция должна быть видимой пользователям карты и ссылаться на страницу авторского права OSM: https://www.openstreetmap.org/copyright. В интерфейсе проекта ссылка «© OpenStreetMap» отображается в легенде карты на `frontend/src/pages/NetworkPage.tsx`; URL атрибуции также передаётся слою тайлов в `LiveMap.tsx`.
- При создании и распространении производной базы данных на основе данных OSM могут применяться условия share-alike ODbL. Наличие карты или отображение стандартных растровых тайлов само по себе не означает, что исходные данные проекта автоматически лицензированы по ODbL.
- Стандартный сервер тайлов предоставляется «как есть», без гарантии доступности или пригодности для конкретной нагрузки. Для значительного, коммерческого или производственного трафика следует выбрать провайдера тайлов с подходящими условиями либо развернуть собственную инфраструктуру, соблюдая ODbL и условия соответствующего провайдера.

### Замечание о лицензии React-Leaflet

На момент фиксации зависимостей `frontend/package-lock.json` указывает `Hippocratic-2.1` для `react-leaflet@5.0.0` и `@react-leaflet/core@3.0.0`. Это не MIT: лицензия содержит условия, связанные с соблюдением прав человека, и не относится к стандартным лицензиям, одобренным OSI. Перед передачей или распространением продукта нужно изучить полный текст этой лицензии и подтвердить совместимость с целевым сценарием использования. Если нужна только OSI-approved лицензия, следует отдельно оценить замену этих библиотек или использование версии/альтернативы с подходящей лицензией; не считать их MIT по умолчанию.

## 6. Docker/OCI runtime

Используются официальные образы:

- `postgres:17-alpine` — PostgreSQL License (PostgreSQL project), плюс Alpine Linux пакеты с лицензиями Apache-2.0 / MIT / GPL-2.0-compatible, в зависимости от пакетов внутри образа
- `python:3.12-slim-bookworm` — Python Software Foundation License, плюс пакеты Debian/Ubuntu как часть base image
- `node:22-alpine` — MIT/Apache-2.0 и компоненты Alpine base image
- `golang:1.26-bookworm` — Go License (BSD-3-Clause-like) и стандартный toolchain Go

При сборке и распространении контейнеров должны учитываться не только имена образов, но и лицензии всех установленных системных пакетов внутри этих образов.

## 7. Рекомендуемые действия при распространении

Для релизной сборки и передачи проекта третьим лицам рекомендуется:

1. Сохранять оригинальные тексты лицензий зависимостей в отдельной директории, например `third_party/` или `licenses/`.
2. Для каждого контейнерного слоя учитывать пакеты внутри base image и их лицензии.
3. Проверять точные версии пакетов после `go mod tidy`, `pip install` и `npm install`.
4. При выпуске коммерческого продукта сохранять список используемых библиотек и их лицензии в артефактах сборки.
5. Если проект поставляется клиенту в сборке, добавить сканирование зависимостей (например, `go-licenses`, `pip-audit`, `npm-license-crawler`, `syft`/`trivy` для OCI-образов).

## 8. Краткое резюме

Проект использует компоненты с лицензиями MIT, Apache-2.0, BSD и другими. В частности, картографический стек включает Leaflet под BSD-2-Clause, данные OpenStreetMap под ODbL 1.0, а текущие зафиксированные версии React-Leaflet и `@react-leaflet/core` помечены Hippocratic-2.1. Поэтому нельзя считать все зависимости проекта стандартными MIT/Apache/BSD или делать вывод о полной лицензионной совместимости только по этому краткому перечню. Для распространения нужно проверять точные версии и полные тексты лицензий, условия OSM и правила выбранного сервера тайлов.

## 9. Источники проверки

- `backend/go.mod`
- `frontend/package.json`
- `ml/requirements.txt`
- `ml/requirements-onnx.txt`
- upstream metadata проектов библиотек и официальных Docker-образов
