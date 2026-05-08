# Парсер логов

Микросервис на Go 1.22, который принимает архивы с логами из папки `data/`, парсит секции `nodes`, `ports`, `nodes_info`, сохраняет результат в PostgreSQL и отдает REST API для топологии.

## Возможности

- `POST /api/v1/parse/` — принять путь до архива и запустить парсинг
- `GET /api/v1/topology/{log_id}` — получить узлы, порты и группы топологии
- `GET /api/v1/node/{node_id}` — получить детали узла
- `GET /api/v1/port/{node_id}` — получить порты узла
- `GET /api/v1/log/{log_id}` — получить мета-информацию о логе

## Формат лога

Сервис принимает текстовый файл внутри архива. Поддерживаются секции:

```text
[nodes]
id=node-1 type=host name=host-1 group=compute
id=node-2 type=switch name=switch-1 group=core

[ports]
id=port-1 node_id=node-1 name=eth0 speed=1g peer_node_id=node-2 peer_port_id=port-2
id=port-2 node_id=node-2 name=xe-0/0/1 speed=1g peer_node_id=node-1 peer_port_id=port-1

[nodes_info]
node_id=node-1 vendor=Acme model=H1 description=Primary host
node_id=node-2 vendor=NetCorp model=S1 description=Core switch
```

Строки также могут быть JSON-объектами в тех же секциях.

## Быстрый старт

```bash
docker compose up -d --build
```

Если host-порт был выбран автоматически, его можно посмотреть через `docker compose port app 8080`.

По умолчанию приложение слушает `8080`. Порт приложения можно переопределить переменной `PORT`.

По умолчанию Compose публикует сервис на свободный host-порт. Если нужен фиксированный `8080`, задайте `HOST_PORT=8080` и убедитесь, что порт свободен.

Для локальной проверки в репозитории уже лежит пример архива: `data/example.zip`.

## Пример запроса

```bash
curl -X POST http://localhost:8080/api/v1/parse/ \
  -H 'Content-Type: application/json' \
  -d '{"path":"data/example.zip"}'
```

## Пример ответа

```json
{"log_id":1}
```

## curl для API

```bash
curl http://localhost:8080/api/v1/log/1
curl http://localhost:8080/api/v1/node/1
curl http://localhost:8080/api/v1/port/1
curl http://localhost:8080/api/v1/topology/1
```

## Docker Compose

В корне проекта должны быть `README.md` и `docker-compose.yml`. Сервис `app` монтирует папку `data/`, откуда читаются архивы.

## Дополнительно

- `postman_collection.json` — готовая коллекция запросов.
- `.env.example` — пример переменных окружения.
