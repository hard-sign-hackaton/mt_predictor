# MT Predictor — Dispatcher BI Mock

Функциональный desktop-прототип диспетчерской панели. Данные и live-обновления замоканы, действия сохраняются локально в браузере.

## Запуск

```bash
docker compose up --build
```

Открыть <http://localhost:5173>.

## Проверки

```bash
docker compose run --rm dashboard npm run lint
docker compose run --rm dashboard npm test
docker compose run --rm dashboard npm run build
```

Кнопка «Сбросить демо» в верхней панели очищает сохранённое состояние.
