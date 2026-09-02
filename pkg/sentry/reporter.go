package sentry

import (
	"context"

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

func (r *Reporter) Capture(ctx context.Context, err error, opts errorreporter.Options) {
	sentrygo.WithScope(func(scope *sentrygo.Scope) {
		if appInfo := coreCtx.GetAppInfoFromContext(ctx); appInfo != nil {
			scope.SetTag("environment", appInfo.AppEnv)
			scope.SetTag("service", appInfo.ServiceName)
		}

		if len(opts.Tags) > 0 {
			scope.SetTags(opts.Tags)
		}

		for key, value := range opts.Extra {
			scope.SetContext(key, toSentryContext(value))
		}

		scope.SetLevel(mapLevel(opts.Level))

		sentrygo.CaptureException(err)
	})
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
