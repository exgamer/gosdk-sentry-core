package app_test

// Файл идёт после sentry_kernel_verify_test.go: тесты с SENTRY_DSN регистрируют Reporter
// (errorreporter.IsConfigured залипает на процесс), а тест пустого DSN проверяется первым.

import (
	"bytes"
	"context"
	stdlog "log"
	"os"
	"strings"
	"testing"
	"time"

	coreApp "github.com/exgamer/gosdk-core/pkg/app"
	"github.com/exgamer/gosdk-core/pkg/logger"
	sentryapp "github.com/exgamer/gosdk-sentry-core/pkg/app"
	sentrygo "github.com/getsentry/sentry-go"
)

const testDSN = "https://1234567890abcdef1234567890abcdef@o447951.ingest.sentry.io/1234567"

func TestSentryKernel_InitOptionsAndMaskedDump(t *testing.T) {
	cases := []struct {
		name    string
		dsn     string
		wantEnv string
	}{
		{name: "with dsn", dsn: testDSN, wantEnv: "prod"},
		{name: "without dsn", dsn: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SENTRY_DSN", tc.dsn)
			t.Setenv("APP_ENV", "prod")
			t.Setenv("APP_VERSION", "1.4.2")
			t.Setenv("CONTAINER_NAME", "orders-7f9")
			t.Setenv("LOG_LEVEL", "debug")
			t.Setenv("SENTRY_ENVIRONMENT", "")
			t.Setenv("SENTRY_RELEASE", "")

			var out bytes.Buffer
			logger.SetLogger(stdlog.New(&out, "", 0))
			t.Cleanup(func() {
				logger.SetLogger(stdlog.New(os.Stdout, "", 0))
				logger.SetLevel(logger.LevelInfo)
			})

			a := coreApp.NewApp().SetEnvFiles(t.TempDir() + "/absent.env")
			if err := a.RegisterAndInitKernels(&sentryapp.SentryKernel{}); err != nil {
				t.Fatalf("init: %v", err)
			}

			if strings.Contains(out.String(), "1234567890abcdef") {
				t.Fatalf("debug dump prints DSN: %s", out.String())
			}

			if tc.dsn == "" {
				return
			}

			if !strings.Contains(out.String(), `"sentry_dsn":"***"`) {
				t.Fatalf("dump = %s, want masked sentry_dsn", out.String())
			}

			opts := sentrygo.CurrentHub().Client().Options()
			if opts.Environment != tc.wantEnv || opts.Release != "1.4.2" || opts.ServerName != "orders-7f9" {
				t.Fatalf("options env=%q release=%q server=%q", opts.Environment, opts.Release, opts.ServerName)
			}
		})
	}
}

// ctxTransport Запоминает ctx Flush и ждёт его отмены.
type ctxTransport struct {
	gotDeadline chan bool
}

func (t *ctxTransport) Configure(sentrygo.ClientOptions) {}
func (t *ctxTransport) Flush(time.Duration) bool         { return true }
func (t *ctxTransport) FlushWithContext(ctx context.Context) bool {
	_, ok := ctx.Deadline()
	t.gotDeadline <- ok
	<-ctx.Done()

	return false
}
func (t *ctxTransport) SendEvent(*sentrygo.Event) {}
func (t *ctxTransport) Close()                    {}

func TestSentryKernel_StopFlushesWithinCtx(t *testing.T) {
	cases := []struct {
		name    string
		ctx     func() (context.Context, context.CancelFunc)
		maxWait time.Duration
	}{
		{
			name: "stop ctx deadline",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			maxWait: time.Second,
		},
		{
			name:    "no deadline - bounded",
			ctx:     func() (context.Context, context.CancelFunc) { return context.Background(), func() {} },
			maxWait: 3 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &ctxTransport{gotDeadline: make(chan bool, 1)}

			client, err := sentrygo.NewClient(sentrygo.ClientOptions{Dsn: testDSN, Transport: tr})
			if err != nil {
				t.Fatalf("client: %v", err)
			}

			prev := sentrygo.CurrentHub().Client()
			sentrygo.CurrentHub().BindClient(client)
			t.Cleanup(func() { sentrygo.CurrentHub().BindClient(prev) })

			ctx, cancel := tc.ctx()
			defer cancel()

			start := time.Now()
			if err := (&sentryapp.SentryKernel{}).Stop(ctx); err != nil {
				t.Fatalf("stop: %v", err)
			}

			if elapsed := time.Since(start); elapsed > tc.maxWait {
				t.Fatalf("stop took %s, want <= %s", elapsed, tc.maxWait)
			}

			if !<-tr.gotDeadline {
				t.Fatal("flush ctx has no deadline")
			}
		})
	}
}
