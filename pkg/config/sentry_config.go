package config

// SentryConfig - настройки подключения к Sentry, читаются из ENV.
type SentryConfig struct {
	SentryDsn string `mapstructure:"SENTRY_DSN" json:"sentry_dsn"`
}

// dsnMask То, что печатается вместо DSN.
const dsnMask = "***"

// Masked Копия для печати (дамп конфига в debug): DSN заменён маской.
func (c SentryConfig) Masked() SentryConfig {
	if c.SentryDsn != "" {
		c.SentryDsn = dsnMask
	}

	return c
}
