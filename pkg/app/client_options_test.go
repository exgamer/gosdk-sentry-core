package app

import (
	"testing"

	baseConfig "github.com/exgamer/gosdk-core/pkg/config"
)

func TestClientOptions(t *testing.T) {
	base := &baseConfig.BaseConfig{AppEnv: "prod", Version: "1.4.2", ContainerName: "orders-7f9"}

	cases := []struct {
		name    string
		base    *baseConfig.BaseConfig
		envVars map[string]string
		wantEnv string
		wantRel string
		wantSrv string
	}{
		{name: "from base config", base: base, wantEnv: "prod", wantRel: "1.4.2", wantSrv: "orders-7f9"},
		{name: "no base config", base: nil},
		{name: "empty fields", base: &baseConfig.BaseConfig{}},
		{
			name:    "sentry variables win",
			base:    base,
			envVars: map[string]string{"SENTRY_ENVIRONMENT": "staging", "SENTRY_RELEASE": "abc"},
			wantSrv: "orders-7f9",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SENTRY_ENVIRONMENT", "")
			t.Setenv("SENTRY_RELEASE", "")

			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			opts := clientOptions("https://key@example.com/1", tc.base)

			if opts.Environment != tc.wantEnv || opts.Release != tc.wantRel || opts.ServerName != tc.wantSrv {
				t.Fatalf("got env=%q release=%q server=%q, want %q %q %q",
					opts.Environment, opts.Release, opts.ServerName, tc.wantEnv, tc.wantRel, tc.wantSrv)
			}

			if opts.Dsn != "https://key@example.com/1" || !opts.AttachStacktrace {
				t.Fatalf("dsn/stacktrace lost: %+v", opts)
			}
		})
	}
}
