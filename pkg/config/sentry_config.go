package config

// SentryConfig - настройки подключения к Sentry, читаются из ENV.
type SentryConfig struct {
	SentryDsn string `mapstructure:"SENTRY_DSN" json:"sentry_dsn"`
}
