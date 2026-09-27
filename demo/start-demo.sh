#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "${script_dir}/.." && pwd)"
emulator_archive="${project_dir}/../ndtp-telemetry-emulator.tar"
traffic_file="${project_dir}/../validate/traffic.csv"

cd "${project_dir}"

mode="${1:-full}"

for dependency in docker curl; do
  if ! command -v "${dependency}" >/dev/null 2>&1; then
    echo "Не найдена обязательная команда: ${dependency}" >&2
    exit 1
  fi
done

if [[ "${mode}" != "mock" && ! -f "${traffic_file}" ]]; then
  echo "Не найден датасет для CSV replay: ${traffic_file}" >&2
  echo "Репозиторий должен находиться в Project/mt_predictor рядом с каталогом dataset из раздачи." >&2
  exit 1
fi

if [[ "${mode}" != "mock" ]] && ! docker image inspect ndtp-telemetry-emulator:1.0 >/dev/null 2>&1; then
  if [[ ! -f "${emulator_archive}" ]]; then
    echo "Не найден образ эмулятора: ${emulator_archive}" >&2
    exit 1
  fi
  echo "Загружаю официальный образ эмулятора..."
  docker load -i "${emulator_archive}"
fi

if [[ "${mode}" == "mock" ]]; then
  echo "Запускаю контролируемый mock-набор..."
  docker compose --profile mock up -d --build postgres ml backend frontend mock-feeder
else
  echo "Запускаю backend, frontend, CSV replay и официальный эмулятор..."
  docker compose --profile official-emulator up -d --build
fi

if [[ "${mode}" != "mock" ]]; then
echo "Жду готовности HTTP API эмулятора..."
emulator_ready=false
for _ in {1..45}; do
  http_code="$(curl -sS -o /dev/null -w '%{http_code}' http://localhost:18080/api/cells 2>/dev/null || true)"
  if [[ "${http_code}" == "200" ]]; then
    emulator_ready=true
    break
  fi
  sleep 2
done

if [[ "${emulator_ready}" != "true" ]]; then
  echo "Эмулятор не стал готов за 90 секунд. Проверьте: docker compose logs official-emulator" >&2
  exit 1
fi

echo "Настраиваю пять ТС официального эмулятора..."
curl -fsS -X POST http://localhost:18080/api/config \
  -H 'Content-Type: application/json' \
  --data-binary @demo/official-emulator-config.json >/dev/null
fi

backend_ready=false
for _ in {1..20}; do
  if curl -fsS http://localhost:8080/health >/dev/null 2>&1; then
    backend_ready=true
    break
  fi
  sleep 1
done

if [[ "${backend_ready}" != "true" ]]; then
  echo "Backend не отвечает. Проверьте: docker compose logs backend" >&2
  exit 1
fi

if [[ "${mode}" == "mock" ]]; then
  echo "Загружаю сценарии диагностических причин..."
  curl -fsS -X POST http://localhost:8080/api/v1/demo/scenarios \
    -H 'Content-Type: application/json' \
    --data-binary @backend/data/mock_scenarios.json >/dev/null
fi

echo "Готово: http://localhost:5173"
echo "Health: http://localhost:8080/health"
