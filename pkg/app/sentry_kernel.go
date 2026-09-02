package app

import (
	"context"
	"time"

	coreApp "github.com/exgamer/gosdk-core/pkg/app"
	baseConfig "github.com/exgamer/gosdk-core/pkg/config"
	"github.com/exgamer/gosdk-core/pkg/di"
	"github.com/exgamer/gosdk-core/pkg/errorreporter"
	"github.com/exgamer/gosdk-core/pkg/logger"
	"github.com/exgamer/gosdk-sentry-core/pkg/config"
	"github.com/exgamer/gosdk-sentry-core/pkg/sentry"
	sentrygo "github.com/getsentry/sentry-go"
)

const SentryKernelName = "sentry"

// SentryKernel - единственное место в приложении, которое инициализирует
// sentry-go и регистрирует его как errorreporter.Reporter. Не зависит от
// HttpKernel/RabbitKernel и не требует их - работает для любой комбинации
// кернелов (HTTP, Rabbit-консьюмер, консольная команда).
//
// Если SENTRY_DSN не задан - Init отрабатывает без ошибки, errorreporter
// остаётся no-op (события никуда не отправляются, но код, вызывающий
// errorreporter.Capture/CaptureSoft/CaptureError, продолжает работать как
// обычно).
type SentryKernel struct {
	Config *config.SentryConfig
}

func (k *SentryKernel) Name() string {
	return SentryKernelName
}

func (k *SentryKernel) Init(a *coreApp.App) error {
	cfg := &config.SentryConfig{}
	if err := baseConfig.InitConfig(cfg); err != nil {
		return err
	}

	k.Config = cfg
	logger.Dump(a.GetContext(), cfg)
	di.Register(a.Container, k.Config)

	if cfg.SentryDsn == "" {
		logger.Info(a.GetContext(), "sentry: SENTRY_DSN is empty, error reporting disabled")

		return nil
	}

	if err := sentrygo.Init(sentrygo.ClientOptions{
		AttachStacktrace: true,
		TracesSampleRate: 1.0,
		Dsn:              cfg.SentryDsn,
	}); err != nil {
		return err
	}

	errorreporter.SetReporter(sentry.NewReporter())

	return nil
}

func (k *SentryKernel) Start(a *coreApp.App) error {
	return nil
}

func (k *SentryKernel) Stop(ctx context.Context) error {
	_ = sentrygo.Flush(2 * time.Second)

	return nil
}
