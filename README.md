# gosdk-sentry-core

Адаптер Sentry поверх `errorreporter` из `gosdk-core`. Единственный пакет
в SDK, который импортирует `sentry-go` напрямую. Ни `gosdk-http-core`,
ни `gosdk-rabbit-core`, ни код приложения про Sentry ничего не знают -
они зовут только `errorreporter.Capture`/`CaptureSoft`/`CaptureError`.

## Установка

```bash
go get github.com/exgamer/gosdk-sentry-core
```

## Подключение

```go
import (
    "github.com/exgamer/gosdk-core/pkg/app"
    sentrykernel "github.com/exgamer/gosdk-sentry-core/pkg/app"
)

appInstance.RegisterAndInitKernels(
    &sentrykernel.SentryKernel{},
    // ... остальные кернелы (Http, Rabbit, Postgres)
)
```

Порядок регистрации кернелов не важен - `SentryKernel.Init` только
регистрирует `Reporter` глобально, а реальные отправки происходят позже
(на запросах/сообщениях), когда все кернелы уже проинициализированы.

## ENV

```bash
SENTRY_DSN=https://xxx@sentry.io/yyy
```

Если `SENTRY_DSN` не задан, `SentryKernel.Init` не возвращает ошибку -
приложение стартует как обычно, просто `errorreporter` остаётся no-op.

## Что делает

- `sentry.Init(...)` с `AttachStacktrace: true`, `TracesSampleRate: 1.0` (как раньше в `HttpKernel`) и
  данными приложения из `BaseConfig` gosdk-core: `Environment` — `APP_ENV`, `Release` — `APP_VERSION`,
  `ServerName` — `CONTAINER_NAME` (пустое значение не задаётся; заданные `SENTRY_ENVIRONMENT` и
  `SENTRY_RELEASE` — переменные самого sentry-go — остаются в силе).
- В debug-дампе конфига (`LOG_LEVEL=debug`) DSN заменён на `***`.
- Регистрирует `errorreporter.Reporter`, который на каждый `Capture`:
  - берёт копию hub запроса из контекста (`sentry.GetHubFromContext`; данные запроса из его scope
    сохраняются), без него — копию глобального hub: теги и контексты события не попадают в чужие события
    при параллельных запросах;
  - ставит теги `environment`/`service` из `AppInfo` в контексте;
  - ставит тег `correlation_id`, если в контексте есть сквозной id корреляции (`sdkctx.CorrelationID`, gosdk-core;
    его кладут http-core, rabbit, kafka, console) — по нему событие находится в логах всей цепочки;
  - добавляет теги и контексты из `errorreporter.Options`;
  - маппит `errorreporter.Level` в `sentry.Level`;
  - отправляет ошибку целиком — всю цепочку `Unwrap` (и конверт вроде `*exception.HttpException`, и
    настоящую причину вроде `*pgconn.PgError`), без внешней обёртки-маркера errorreporter.
- На `Stop` ждёт отправки событий в пределах `ctx` остановки (`sentry.FlushWithContext`); без дедлайна у
  `ctx` — не дольше 2 с.

## Важно: breaking change при апгрейде с v1 SDK

Раньше `gosdk-http-core` сам инициализировал Sentry, если было выставлено
`SENTRY_DSN`. Начиная с `gosdk-http-core/v2` / `gosdk-rabbit-core/v2` это
поведение убрано - `HttpKernel`/`Consumer` только зовут
`errorreporter.Capture`, а инициализация Sentry - явная зона
ответственности `SentryKernel`. Если не зарегистрировать `SentryKernel`,
события в Sentry просто не будут уходить (без ошибок, без паники - тихо).

При переходе на v2 обязательно добавьте `SentryKernel` в список кернелов,
если сервис использует Sentry.
