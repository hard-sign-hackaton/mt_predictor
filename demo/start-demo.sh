#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "${script_dir}/.." && pwd)"
emulator_archive="${project_dir}/../../dataset/ndtp-telemetry-emulator.tar"
mode="${1:-mock}"

cd "${project_dir}"

for dependency in docker curl; do
  if ! command -v "${dependency}" >/dev/null 2>&1; then
    echo "Не найдена обязательная команда: ${dependency}" >&2
    exit 1
  fi
done

local_curl() {
  curl --noproxy '*' "$@"
}

case "${mode}" in
  mock)
    profile="mock"
    ;;
  emulator)
    profile="official-emulator"
    if ! docker image inspect ndtp-telemetry-emulator:1.0 >/dev/null 2>&1; then
      if [[ ! -f "${emulator_archive}" ]]; then
        echo "Не найден образ эмулятора: ${emulator_archive}" >&2
        exit 1
      fi
      echo "Загружаю официальный образ эмулятора..."
      docker load -i "${emulator_archive}"
    fi
    ;;
  *)
    echo "Использование: $0 [mock|emulator]" >&2
    exit 2
    ;;
esac

echo "Запускаю стенд с источником телеметрии: ${mode}..."
docker compose --profile "${profile}" up -d --build --remove-orphans

backend_ready=false
for _ in {1..45}; do
  if local_curl -fsS http://localhost:8080/health >/dev/null 2>&1; then
    backend_ready=true
    break
  fi
  sleep 2
done

if [[ "${backend_ready}" != "true" ]]; then
  echo "Backend не отвечает. Проверьте: docker compose logs backend" >&2
  exit 1
fi

if [[ "${mode}" == "mock" ]]; then
  echo "Загружаю детерминированные сценарии инцидентов..."
  local_curl -fsS -X POST http://localhost:8080/api/v1/demo/scenarios \
    -H 'Content-Type: application/json' \
    --data-binary @backend/data/mock_scenarios.json
  echo
else
  echo "Жду готовности HTTP API эмулятора..."
  emulator_ready=false
  for _ in {1..45}; do
    http_code="$(local_curl -sS -o /dev/null -w '%{http_code}' http://localhost:18080/api/cells 2>/dev/null || true)"
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

  echo "Настраиваю тестовые ТС официального эмулятора..."
  local_curl -fsS -X POST http://localhost:18080/api/config \
    -H 'Content-Type: application/json' \
    --data-binary @demo/official-emulator-config.json >/dev/null
fi

echo "Готово: http://localhost:5173"
echo "Инциденты: http://localhost:5173/incidents"
echo "Health: http://localhost:8080/health"
echo "Health: http://localhost:8080/health"
