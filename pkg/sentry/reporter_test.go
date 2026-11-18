package sentry

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	coreCtx "github.com/exgamer/gosdk-core/pkg/context"
	"github.com/exgamer/gosdk-core/pkg/errorreporter"
	sentrygo "github.com/getsentry/sentry-go"
)

// memTransport Транспорт sentry-go в память.
type memTransport struct {
	mu     sync.Mutex
	events []*sentrygo.Event
}

func (t *memTransport) Configure(sentrygo.ClientOptions)      {}
func (t *memTransport) Flush(time.Duration) bool              { return true }
func (t *memTransport) FlushWithContext(context.Context) bool { return true }
func (t *memTransport) Close()                                {}
func (t *memTransport) SendEvent(e *sentrygo.Event) {
	t.mu.Lock()
	t.events = append(t.events, e)
	t.mu.Unlock()
}
func (t *memTransport) all() []*sentrygo.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*sentrygo.Event(nil), t.events...)
}

func newHub(t *testing.T) (*sentrygo.Hub, *memTransport) {
	t.Helper()

	tr := &memTransport{}

	client, err := sentrygo.NewClient(sentrygo.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: tr,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	return sentrygo.NewHub(client, sentrygo.NewScope()), tr
}

// useGlobalHub Подменяет клиент глобального hub на клиент в память до конца теста.
func useGlobalHub(t *testing.T) *memTransport {
	t.Helper()

	hub, tr := newHub(t)
	prev := sentrygo.CurrentHub().Client()

	sentrygo.CurrentHub().BindClient(hub.Client())
	t.Cleanup(func() { sentrygo.CurrentHub().BindClient(prev) })

	return tr
}

// TestReporter_ScopeIsolation Параллельные Capture: у каждого события только свои теги и контекст.
func TestReporter_ScopeIsolation(t *testing.T) {
	cases := []struct {
		name     string
		withHub  bool
		parallel int
	}{
		{name: "global hub", parallel: 200},
		{name: "hub from ctx", withHub: true, parallel: 200},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := useGlobalHub(t)
			ctx := context.Background()

			if tc.withHub {
				var hub *sentrygo.Hub

				hub, tr = newHub(t)
				hub.Scope().SetTag("request", "r-1")
				ctx = sentrygo.SetHubOnContext(ctx, hub)
			}

			r := NewReporter()

			var wg sync.WaitGroup

			for i := range tc.parallel {
				wg.Add(1)

				go func() {
					defer wg.Done()

					id := strconv.Itoa(i)
					r.Capture(ctx, errors.New("err-"+id), errorreporter.Options{
						Level: errorreporter.LevelError,
						Tags:  map[string]string{"id": id},
						Extra: map[string]any{"payload": map[string]any{"id": id}},
					})
				}()
			}

			wg.Wait()

			events := tr.all()
			if len(events) != tc.parallel {
				t.Fatalf("events = %d, want %d", len(events), tc.parallel)
			}

			for _, e := range events {
				id := e.Tags["id"]
				if e.Exception[len(e.Exception)-1].Value != "err-"+id {
					t.Fatalf("event %q has tag id=%s: tags leaked between events", e.Exception[0].Value, id)
				}

				if got := e.Contexts["payload"]["id"]; got != id {
					t.Fatalf("event id=%s has payload id=%v", id, got)
				}

				if tc.withHub && e.Tags["request"] != "r-1" {
					t.Fatalf("event id=%s lost request hub scope: tags=%v", id, e.Tags)
				}
			}

			if tc.withHub {
				// scope hub запроса не изменился
				ev := sentrygo.NewEvent()
				sentrygo.GetHubFromContext(ctx).Scope().ApplyToEvent(ev, nil, nil)

				if _, leaked := ev.Tags["id"]; leaked {
					t.Fatalf("request hub scope got event tag: %v", ev.Tags)
				}
			}
		})
	}
}

type rootErr struct{}

func (rootErr) Error() string { return "pg: duplicate key" }

func TestReporter_ErrorChain(t *testing.T) {
	root := rootErr{}
	wrapped := fmt.Errorf("save order: %w", root)

	cases := []struct {
		name      string
		err       error
		opts      errorreporter.Options
		wantTypes []string // от причины к внешней ошибке
		wantTop   string
	}{
		{
			name:      "chain kept",
			err:       wrapped,
			wantTypes: []string{"sentry.rootErr", "*fmt.wrapError"},
			wantTop:   "save order: pg: duplicate key",
		},
		{
			name:      "reported marker dropped on Force",
			err:       errorreporter.CaptureError(context.Background(), wrapped, nil),
			opts:      errorreporter.Options{Force: true},
			wantTypes: []string{"sentry.rootErr", "*fmt.wrapError"},
			wantTop:   "save order: pg: duplicate key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := useGlobalHub(t)

			NewReporter().Capture(context.Background(), tc.err, tc.opts)

			events := tr.all()
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}

			exc := events[0].Exception

			var types []string
			for _, e := range exc {
				types = append(types, e.Type)
			}

			if fmt.Sprint(types) != fmt.Sprint(tc.wantTypes) {
				t.Fatalf("exception types = %v, want %v", types, tc.wantTypes)
			}

			if top := exc[len(exc)-1].Value; top != tc.wantTop {
				t.Fatalf("top exception = %q, want %q", top, tc.wantTop)
			}
		})
	}
}

func TestWithoutReportedMarker(t *testing.T) {
	base := errors.New("base")
	reported := errorreporter.CaptureError(context.Background(), base, nil)
	outer := fmt.Errorf("handler: %w", reported)

	cases := []struct {
		name string
		in   error
		want error
	}{
		{name: "plain", in: base, want: base},
		{name: "marker on top", in: reported, want: base},
		{name: "marker inside keeps outer context", in: outer, want: outer},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := withoutReportedMarker(tc.in); got != tc.want {
				t.Fatalf("got %v (%T), want %v (%T)", got, got, tc.want, tc.want)
			}
		})
	}
}

// TestReporter_CorrelationIDTag Id корреляции ctx — тег correlation_id; нет id — нет тега; явный тег главнее.
func TestReporter_CorrelationIDTag(t *testing.T) {
	cases := []struct {
		name    string
		ctx     context.Context
		opts    errorreporter.Options
		want    string
		wantSet bool
	}{
		{name: "from ctx", ctx: coreCtx.WithCorrelationID(context.Background(), "corr-1"), want: "corr-1", wantSet: true},
		{name: "no id", ctx: context.Background()},
		{
			name:    "explicit tag wins",
			ctx:     coreCtx.WithCorrelationID(context.Background(), "corr-1"),
			opts:    errorreporter.Options{Tags: map[string]string{"correlation_id": "own"}},
			want:    "own",
			wantSet: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := useGlobalHub(t)

			NewReporter().Capture(tc.ctx, errors.New("boom"), tc.opts)

			events := tr.all()
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}

			got, ok := events[0].Tags["correlation_id"]
			if ok != tc.wantSet || got != tc.want {
				t.Fatalf("tag correlation_id = %q (set %v), want %q (set %v)", got, ok, tc.want, tc.wantSet)
			}
		})
	}
}
