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

Файл: `frontend/package.json`

Основные зависимости и типовые лицензии:

- `react` — MIT
- `react-dom` — MIT
- `react-router-dom` — MIT
- `leaflet` — BSD-2-Clause
- `react-leaflet` — MIT
- `vite` — MIT
- `typescript` — Apache-2.0
- `@vitejs/plugin-react` — MIT
- `@types/react`, `@types/react-dom`, `@types/node` — MIT
- `eslint` — MIT
- `vitest` — MIT
- `@testing-library/*` — MIT
- `jsdom` — MIT

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

Проект в целом использует стандартный набор открытых библиотек с доминирующими лицензиями MIT, Apache-2.0 и BSD-3-Clause. Для коммерческого распространения и публичного релиза рекомендуется дополнительно сохранить точные upstream license texts и обновлять этот файл при изменении зависимостей.

## 9. Источники проверки

- `backend/go.mod`
- `frontend/package.json`
- `ml/requirements.txt`
- `ml/requirements-onnx.txt`
- upstream metadata проектов библиотек и официальных Docker-образов

Если вы хотите, можно дополнительно сгенерировать более строгий `THIRD_PARTY_LICENSES`-список в формате SPDX, подходящий для аудитных целей, но это требует отдельной подготовки по точным версиям всех зависимостей.
