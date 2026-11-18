package app

import (
	"context"
	"os"
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

// flushTimeout Предел ожидания отправки событий на Stop, если у ctx остановки нет своего дедлайна.
const flushTimeout = 2 * time.Second

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
	logger.Dump(a.GetContext(), cfg.Masked())
	di.Register(a.Container, k.Config)

	if cfg.SentryDsn == "" {
		logger.Info(a.GetContext(), "sentry: SENTRY_DSN is empty, error reporting disabled")

		return nil
	}

	if err := sentrygo.Init(clientOptions(cfg.SentryDsn, a.BaseConfig)); err != nil {
		return err
	}

	errorreporter.SetReporter(sentry.NewReporter())

	return nil
}

func (k *SentryKernel) Start(a *coreApp.App) error {
	return nil
}

// Stop ждёт отправки накопленных событий в пределах ctx остановки; ctx без дедлайна — не дольше flushTimeout.
func (k *SentryKernel) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, flushTimeout)
		defer cancel()
	}

	_ = sentrygo.FlushWithContext(ctx)

	return nil
}

// clientOptions Настройки sentry-go: окружение — APP_ENV, релиз — APP_VERSION, имя сервера — CONTAINER_NAME
// из BaseConfig (пустое значение не задаётся: sentry-go берёт своё значение по умолчанию). Заданные
// SENTRY_ENVIRONMENT и SENTRY_RELEASE (переменные самого sentry-go) остаются в силе.
func clientOptions(dsn string, base *baseConfig.BaseConfig) sentrygo.ClientOptions {
	opts := sentrygo.ClientOptions{
		AttachStacktrace: true,
		TracesSampleRate: 1.0,
		Dsn:              dsn,
	}

	if base == nil {
		return opts
	}

	if os.Getenv("SENTRY_ENVIRONMENT") == "" {
		opts.Environment = base.AppEnv
	}

	if os.Getenv("SENTRY_RELEASE") == "" {
		opts.Release = base.Version
	}

	opts.ServerName = base.ContainerName

	return opts
}
