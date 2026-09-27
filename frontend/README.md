# MT Predictor — live-карта

React-клиент рабочей карты. Он получает статический каталог и live-телеметрию от backend; моковые инциденты и `localStorage` больше не используются.

## Запуск

```bash
cd ..
docker compose up --build
```

Открыть <http://localhost:5173>.

## Проверки

```bash
cd ..
docker compose build frontend
docker compose run --rm frontend \
  sh -c 'npm run lint && npm test && npm run build'
```

Полная схема запуска, проверка официального эмулятора и описание API находятся в корневом [`README.md`](../README.md).
