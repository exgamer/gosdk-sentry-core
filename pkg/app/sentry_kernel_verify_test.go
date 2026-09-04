package app_test

import (
	"context"
	"errors"
	"os"
	"testing"

	coreApp "github.com/exgamer/gosdk-core/pkg/app"
	"github.com/exgamer/gosdk-core/pkg/errorreporter"
	sentryapp "github.com/exgamer/gosdk-sentry-core/pkg/app"
)

// TestSentryKernel_EmptyDSN_StaysNoop — без SENTRY_DSN Init не должен падать
// и не должен регистрировать реальный Reporter. ВАЖНО: этот тест должен
// выполниться первым в файле (порядок объявления), т.к. errorreporter.IsConfigured()
// - залипающий на true флаг на весь процесс.
func TestSentryKernel_EmptyDSN_StaysNoop(t *testing.T) {
	if errorreporter.IsConfigured() {
		t.Skip("errorreporter already configured by another test in this run - order-dependent check skipped")
	}

	os.Unsetenv("SENTRY_DSN")

	a := coreApp.NewApp()
	if err := a.RegisterAndInitKernels(&sentryapp.SentryKernel{}); err != nil {
		t.Fatalf("expected no error with empty SENTRY_DSN, got: %v", err)
	}

	if errorreporter.IsConfigured() {
		t.Fatal("expected errorreporter to stay unconfigured (noop) when SENTRY_DSN is empty")
	}

	// вызов не должен падать даже без реального Reporter-а (console-command-safe)
	err := errorreporter.CaptureError(context.Background(), errors.New("boom"), map[string]string{"component": "cli"})
	if err == nil {
		t.Fatal("expected CaptureError to return the (marked) error, got nil")
	}
}

// TestSentryKernel_StandaloneConsoleScenario_ConfiguresReporter — сценарий
// консольной команды: НЕТ HttpKernel/RabbitKernel, только SentryKernel.
// Раньше (до errorreporter) Sentry инициализировался только внутри HttpKernel,
// то есть в чисто консольном/rabbit-only сервисе Sentry не работал вообще.
// Здесь проверяем, что теперь работает независимо.
func TestSentryKernel_StandaloneConsoleScenario_ConfiguresReporter(t *testing.T) {
	os.Setenv("SENTRY_DSN", "https://1234567890abcdef1234567890abcdef@o447951.ingest.sentry.io/1234567")
	defer os.Unsetenv("SENTRY_DSN")

	a := coreApp.NewApp()

	// именно standalone: SentryKernel - единственный зарегистрированный кернел,
	// имитирует консольную команду / cron-джобу без HTTP и без Rabbit.
	if err := a.RegisterAndInitKernels(&sentryapp.SentryKernel{}); err != nil {
		t.Fatalf("unexpected error initializing standalone SentryKernel: %v", err)
	}

	if !errorreporter.IsConfigured() {
		t.Fatal("expected errorreporter.IsConfigured() to be true after SentryKernel.Init with a DSN")
	}

	// "вызов в консольной команде" - тот самый ручной вызов из кода,
	// без всякого HTTP-запроса или Rabbit-сообщения вокруг.
	origErr := errors.New("console job failed: import batch #42")
	reported := errorreporter.CaptureError(context.Background(), origErr, map[string]string{
		"component": "console_command",
		"command":   "import:cities",
	})

	if !errorreporter.WasReported(reported) {
		t.Fatal("expected returned error to be marked as reported")
	}
	if !errors.Is(reported, origErr) {
		t.Fatal("expected wrapped error to still satisfy errors.Is against the original error")
	}

	// graceful stop не должен виснуть/падать (sentry.Flush с таймаутом)
	if err := (&sentryapp.SentryKernel{}).Stop(context.Background()); err != nil {
		t.Fatalf("unexpected error on Stop: %v", err)
	}
}
