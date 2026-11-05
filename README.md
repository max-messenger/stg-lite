# Lite

Минифреймворк для написания небольших ботов в MAX.

Даёт готовый bootstrap приложения: конфиг → логгер → телеметрия → два пула
Postgres (read/write) → Redis → HTTP-роутер → HTTP и System серверы, с
graceful shutdown из коробки.

## Возможности

- Конфигурация через YAML с раскрытием переменных окружения.
- Structured-логирование на zap.
- OTLP-трейсинг (grpc/http) с сэмплером и propagator.
- Готовые обёртки над Postgres (pgxpool) и Redis (go-redis) с
  автоматическим OTel-трейсингом и Prometheus-метриками на каждый вызов.
- Graceful shutdown с приоритетами остановки сервисов.
- HTTP-сервер (`:8080`) на chi + System-сервер (`:23569`) с
  `/health`, `/metrics`, `/debug`.
- Прогон embedded-миграций через sql-migrate.

## Структура проекта

| Путь | Назначение |
| ---- | ---------- |
| `cmd/lite/main.go` | Точка входа: парсит `-c`, вызывает `app.New` → `app.Run` |
| `internal/app/` | Bootstrap приложения (`app.go`, `config.go`), роутер (`router/`) |
| `internal/info.go` | Version-переменные (`CommitTag`, `CommitShortSHA`), инжектятся через `-ldflags` |
| `pkg/server/` | HTTP-сервер (`:8080`) и System-сервер (`:23569`: `/health`, `/metrics`, `/debug`) |
| `pkg/connections/postgres/` | pgxpool + otelpgx, методы `Call` / `RunInTx` |
| `pkg/connections/rediscli/` | go-redis universal client с префиксами ключей |
| `pkg/telemetry/` | OTLP-трейсинг (grpc/http), sampler, propagator |
| `pkg/logger/` | zap-логгер из конфига |
| `pkg/grace/` | Graceful shutdown с приоритетами |
| `pkg/http/` | Middlewares (Metrics/AccessLog/Trace) и HTTP-клиент (otelhttp) |
| `pkg/migrate/` | Прогон embedded-миграций через sql-migrate |
| `db/` | Embed миграций (`migrations.go`, `migrations/`, `conf.yml`) |
| `config/lite/` | YAML-конфиги приложения |

## Архитектура запуска

Поток запуска (`internal/app/app.go`):

```plain
config → logger → telemetry → 2× Postgres-пула (postgres-sel, postgres-upd,
каждый пингуется) → Redis → chi-роутер → HTTP + System серверы
```

Каждый внешний вызов по умолчанию оборачивается OTel-span'ом и
Prometheus-метрикой. Для лейбла ошибки используется `telemetry.ErrLabelValue(err)`.

## Graceful shutdown

Сервисы регистрируются в `pkg/grace` с приоритетом и останавливаются
**в порядке возрастания приоритета** — сервисы с меньшим значением
останавливаются раньше; внутри одной группы остановка идёт конкурентно.

Приоритеты:

1. `PriorityDefault` (0) — используется по умолчанию в `graceful.Add`;
   сюда попадают HTTP/System серверы.
2. `PriorityLow` (1) — инфраструктура: соединения Postgres и Redis.
3. `PriorityLowest` (2) — телеметрия.

Фактический порядок остановки в `app.go`:

```plain
серверы (Default) → соединения postgres/redis (Low) → telemetry (Lowest)
```

Таймаут shutdown — 15 секунд, реакция на сигналы `SIGINT`/`SIGTERM`.

## Работа с Postgres

Пакет `pkg/connections/postgres` — обёртка над `pgxpool` с OTel-спанами и
Prometheus-метриками на каждый вызов. **Не используйте `RawPool()` напрямую**
для запросов в бизнес-логике — при этом теряются трейсинг и метрики.

В приложении поднимаются два раздельных пула:

- `pgSel` (конфиг `postgres-sel`) — **Read-коннект**: только `SELECT`,
  ходит на реплики.
- `pgUpd` (конфиг `postgres-upd`) — **Write-коннект**: все изменения данных
  (`INSERT`/`UPDATE`/`DELETE`) и любые транзакции (`RunInTx`), ходит на мастер.

Правила:

- Чтение — только через `pgSel.Call`; читать через `pgUpd` запрещено.
- Запись и транзакции — только через `pgUpd` (`Call`/`RunInTx`); писать
  через `pgSel` запрещено.
- Если после записи нужно сразу прочитать актуальные данные
  (read-your-writes) — читайте в той же транзакции на `pgUpd`, а не через
  `pgSel` (реплика может отставать).

Один запрос — всегда через `Call`, внутри колбэка доступен `*pgx.Conn`:

```go
err := pgSel.Call(ctx, "get_user", func(ctx context.Context, conn *pgx.Conn) error {
    row := conn.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", id)
    return row.Scan(&name)
})
```

Несколько запросов — всегда через `RunInTx` (одна транзакция, откат при
ошибке или панике). Транзакции — только на `pgUpd`:

```go
err := pgUpd.RunInTx(ctx, "create_order", func(ctx context.Context, conn *pgx.Conn) error {
    if _, err := conn.Exec(ctx, "INSERT INTO orders ..."); err != nil {
        return err
    }
    _, err := conn.Exec(ctx, "UPDATE stock ...")
    return err
}, pgx.TxOptions{})
```

`name` в `Call`/`RunInTx` — snake_case-имя операции, попадает в лейблы
метрик и спаны.

## Работа с Redis

Пакет `pkg/connections/rediscli` — go-redis universal client с префиксом
ключей, метриками и трейсингом. Прямого доступа к клиенту нет — все
операции только через `Call`.

Каждый ключ внутри колбэка нужно пропускать через `keyFormatter` — он
добавляет префикс инстанса; без форматтера ключ уйдёт без префикса:

```go
err := rd.Call(ctx, "get_session", func(ctx context.Context, keyFormatter rediscli.FormatKey, ucl redis.UniversalClient) error {
    val, err := ucl.Get(ctx, keyFormatter("session:"+id)).Result()
    if errors.Is(err, redis.Nil) {
        return nil // ключа нет — не ошибка
    }
    return err
})
```

- Несколько команд на разные ключи в одном `Call` — ок (клиент
  потокобезопасен); для атомарности внутри колбэка используйте
  `ucl.TxPipeline()`.
- `name` — snake_case-имя операции для метрик и спанов.

## Конвенции кода

- Импорт-префикс модуля: `lite/...` (например `lite/pkg/server`).
- Package-per-concern в `pkg/`; в пакете обычно `config.go`, `metrics.go`,
  `options.go` + основной тип.
- Паттерн функциональных опций: `Option func(*T)`, `WithX(...)`.
- Обёртка ошибок: `fmt.Errorf("context: %w", err)`.
- Конструкторы `New` / `NewXxx`, возвращают `(*T, error)`.
- Логирование — zap со структурными полями; логгеры `.Named(...)` по
  подсистемам.
- YAML/JSON-теги в конфиг-структурах — `snake_case` (enforced линтером
  `tagliatelle`).
- Конфиг загружается через `go.uber.org/config` с раскрытием env:
  синтаксис `${VAR:"default"}`.

## Линтинг

`.golangci.yml` строгий (~50 линтеров: `cyclop`, `funlen` 150 строк/50
statements, `gocyclo` complexity 20, `lll` 140 символов, `gosec`, `revive`,
`nlreturn`, `wsl` и т.д.). Форматтеры: `gofmt`, `goimports`, `gci` (секции:
standard, default, localmodule). Всегда прогоняйте `make lint` перед
завершением работы. Тесты из линтинга исключены.
