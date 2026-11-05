# CLAUDE.md

Guidance for working in this repository.

## Структура

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

## Архитектура

Поток запуска (`internal/app/app.go`):
config → logger → telemetry → 2× Postgres-пула (`postgres-sel`, `postgres-upd`,
каждый пингуется) → Redis → chi-роутер → HTTP + System серверы.

**Graceful shutdown (`pkg/grace`):** сервисы группируются по приоритету;
останавливаются в порядке возрастания — **меньшее значение останавливается раньше**,
внутри группы — конкурентно. Константы: `PriorityDefault` (0, `graceful.Add`,
по умолчанию) → `PriorityLow` (1, инфраструктура: БД, кеши) → `PriorityLowest`
(2, telemetry). Фактический порядок в `app.go`: серверы (Default) →
соединения postgres/redis (Low) → telemetry (Lowest). Таймаут shutdown 15s,
сигналы SIGINT/SIGTERM.

**Observability по умолчанию:** каждый внешний вызов оборачивается OTel-span +
Prometheus-метрикой. Для лейбла ошибки использовать `telemetry.ErrLabelValue(err)`.

## Работа с Postgres

Пакет `pkg/connections/postgres` — обёртка над pgxpool с OTel-спанами и
Prometheus-метриками на каждый вызов. **Никогда не используй `RawPool()` напрямую**
для запросов в бизнес-логике — потеряются трейсинг и метрики.

**Обязательно используй два раздельных коннекта — Read и Write.** В приложении
поднимаются два пула (`internal/app/app.go`):

- `pgSel` (конфиг `postgres-sel`) — **Read-коннект**: только чтение (`SELECT`),
  ходит на реплики.
- `pgUpd` (конфиг `postgres-upd`) — **Write-коннект**: все изменения данных
  (`INSERT`/`UPDATE`/`DELETE`) и любые транзакции (`RunInTx`), ходит на мастер.

Правила:

- Чтение — только через `pgSel.Call`, запрещено читать через `pgUpd`.
- Запись и транзакции — только через `pgUpd` (`Call`/`RunInTx`), запрещено
  писать через `pgSel`.
- Если после записи нужно сразу прочитать актуальные данные (read-your-writes) —
  читай в той же транзакции на `pgUpd`, а не через `pgSel` (реплика может
  отставать).

- **Один запрос** — всегда через `Call`. Внутри колбэка работаешь с `*pgx.Conn`.
  Чтение — через `pgSel`:

```go
err := pgSel.Call(ctx, "get_user", func(ctx context.Context, conn *pgx.Conn) error {
    row := conn.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", id)
    return row.Scan(&name)
})
```

- **Несколько запросов** — всегда через `RunInTx` (одна транзакция, откат при
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

- `name` в `Call`/`RunInTx` — snake_case-имя операции, попадает в лейблы метрик и спаны.

## Работа с Redis

Пакет `pkg/connections/rediscli` — go-redis universal client с префиксом ключей,
метриками и трейсингом. **Все операции — только через `Call`**, прямого доступа
к клиенту нет.

**Обязательно:** каждый ключ внутри колбэка пропускай через `keyFormatter` —
он добавляет префикс инстанса. Без форматтера ключ уйдёт без префикса.

```go
err := rd.Call(ctx, "get_session", func(ctx context.Context, keyFormatter rediscli.FormatKey, ucl redis.UniversalClient) error {
    val, err := ucl.Get(ctx, keyFormatter("session:"+id)).Result()
    if errors.Is(err, redis.Nil) {
        return nil // ключа нет — не ошибка
    }
    return err
})
```

- Несколько команд на разные ключи в одном `Call` — ок (клиент потокобезопасен);
  для атомарности внутри колбэка используй `ucl.TxPipeline()`.
- `name` — snake_case-имя операции для метрик и спанов.

## Конвенции кода

- Импорт-префикс модуля: `lite/...` (например `lite/pkg/server`).
- Package-per-concern в `pkg/`; в пакете обычно `config.go`, `metrics.go`, `options.go` + основной тип.
- Паттерн функциональных опций: `Option func(*T)`, `WithX(...)`.
- Обёртка ошибок: `fmt.Errorf("context: %w", err)`.
- Конструкторы `New` / `NewXxx`, возвращают `(*T, error)`.
- Логирование — zap со структурными полями; логгеры `.Named(...)` по подсистемам.
- YAML/JSON-теги в конфиг-структурах — `snake_case` (enforced линтером `tagliatelle`).
- Конфиг загружается через `go.uber.org/config` с раскрытием env: синтаксис `${VAR:"default"}`.

## Линтинг

`.golangci.yml` очень строгий (~50 линтеров: `cyclop`, `funlen` 150 строк/50 statements,
`gocyclo` complexity 20, `lll` 140 символов, `gosec`, `revive`, `nlreturn`, `wsl` и т.д.).
Форматтеры: `gofmt`, `goimports`, `gci` (секции: standard, default, localmodule).
Всегда прогоняй `make lint` перед завершением. Тесты из линтинга исключены.
