package sentry

import (
	"context"
	"errors"
	"time"

	coreCtx "github.com/exgamer/gosdk-core/pkg/context"
	"github.com/exgamer/gosdk-core/pkg/errorreporter"
	sentrygo "github.com/getsentry/sentry-go"
)

// Reporter - реализация errorreporter.Reporter поверх sentry-go.
// Единственное место в SDK, где вызывается sentry.CaptureException -
// любой вызывающий код (http-core, rabbit-core, приложение) ничего не
// знает про Sentry, только про errorreporter.Capture.
type Reporter struct{}

func NewReporter() *Reporter {
	return &Reporter{}
}

// Capture отправляет событие через собственную копию hub: hub запроса из ctx (sentrygo.GetHubFromContext,
// его scope с данными запроса сохраняется), иначе глобальный. Теги и контексты события не попадают в
// чужие события — параллельные Capture из разных запросов не делят scope. Id корреляции ctx
// (coreCtx.CorrelationID) — тег correlation_id; явный тег из opts.Tags главнее.
func (r *Reporter) Capture(ctx context.Context, err error, opts errorreporter.Options) {
	hub := hubFor(ctx)

	hub.WithScope(func(scope *sentrygo.Scope) {
		if appInfo := coreCtx.GetAppInfoFromContext(ctx); appInfo != nil {
			scope.SetTag("environment", appInfo.AppEnv)
			scope.SetTag("service", appInfo.ServiceName)
		}

		if id := coreCtx.CorrelationID(ctx); id != "" {
			scope.SetTag("correlation_id", id)
		}

		if len(opts.Tags) > 0 {
			scope.SetTags(opts.Tags)
		}

		for key, value := range opts.Extra {
			scope.SetContext(key, toSentryContext(value))
		}

		scope.SetLevel(mapLevel(opts.Level))

		hub.CaptureException(withoutReportedMarker(err))
	})
}

// hubFor Копия hub из ctx, иначе копия глобального: scope копии — только этого события.
func hubFor(ctx context.Context) *sentrygo.Hub {
	var hub *sentrygo.Hub

	if ctx != nil {
		hub = sentrygo.GetHubFromContext(ctx)
	}

	if hub == nil {
		hub = sentrygo.CurrentHub()
	}

	return hub.Clone()
}

// Flush - реализация errorreporter.Flusher. Синхронно ждёт отправки
// накопленных в фоновой горутине sentry-go событий - нужно вызывать перед
// выходом из one-shot процессов (консольные команды), у которых нет
// штатного graceful shutdown, где Sentry успел бы отправить всё сам.
func (r *Reporter) Flush(timeout time.Duration) bool {
	return sentrygo.Flush(timeout)
}

// withoutReportedMarker Ошибка целиком, со всей цепочкой Unwrap (sentry-go отправляет цепочку
// исключений: и транспортный конверт вроде *exception.HttpException, и настоящую причину вроде
// *pgconn.PgError), без внешней обёртки-маркера errorreporter «уже отправлено» — у неё нет своих
// данных (сообщение то же, что у обёрнутой ошибки), она приходит при Options.Force.
func withoutReportedMarker(err error) error {
	inner := errors.Unwrap(err)
	if inner == nil || !errorreporter.WasReported(err) || errorreporter.WasReported(inner) {
		return err
	}

	if err.Error() != inner.Error() {
		return err
	}

	return inner
}

func mapLevel(l errorreporter.Level) sentrygo.Level {
	switch l {
	case errorreporter.LevelWarning:
		return sentrygo.LevelWarning
	case errorreporter.LevelFatal:
		return sentrygo.LevelFatal
	case errorreporter.LevelError:
		return sentrygo.LevelError
	default:
		return sentrygo.LevelError
	}
}

// toSentryContext приводит произвольное значение к sentry.Context
// (map[string]interface{}). Если значение уже такая карта - используем
// как есть, иначе заворачиваем в {"value": ...}, чтобы не потерять данные.
func toSentryContext(v any) sentrygo.Context {
	// sentrygo.Context - это алиас map[string]any, поэтому одного case достаточно
	if m, ok := v.(map[string]any); ok {
		return m
	}

	return sentrygo.Context{"value": v}
}
