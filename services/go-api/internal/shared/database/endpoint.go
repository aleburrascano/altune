package database

import "github.com/jackc/pgx/v5/pgconn"

type Endpoint struct {
	Host                 string
	RequiresTLS          bool
	FallsBackToPlaintext bool
}

func ParseEndpoint(databaseURL string) (Endpoint, error) {
	cfg, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return Endpoint{}, err
	}
	endpoint := Endpoint{Host: cfg.Host, RequiresTLS: cfg.TLSConfig != nil}
	for _, fallback := range cfg.Fallbacks {
		if fallback.TLSConfig == nil {
			endpoint.FallsBackToPlaintext = true
		}
	}
	return endpoint, nil
}
