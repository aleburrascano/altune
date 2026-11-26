package database

import "github.com/jackc/pgx/v5/pgconn"

type endpoint struct {
	Host                 string
	RequiresTLS          bool
	FallsBackToPlaintext bool
}

func ParseEndpoint(databaseURL string) (endpoint, error) {
	cfg, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return endpoint{}, err
	}
	ep := endpoint{Host: cfg.Host, RequiresTLS: cfg.TLSConfig != nil}
	for _, fallback := range cfg.Fallbacks {
		if fallback.TLSConfig == nil {
			ep.FallsBackToPlaintext = true
		}
	}
	return ep, nil
}
