package acquisition

import (
	domain "altune/go-api/internal/acquisition/domain"
	catalogpin "altune/go-api/internal/acquisition/internal/adapters/catalogpin"
	ports "altune/go-api/internal/acquisition/internal/core/ports"
)

type SourceUnavailableError = domain.SourceUnavailableError

var ErrSearchDegraded = domain.ErrSearchDegraded

type CatalogPinSource = catalogpin.Source

const KindCatalogPin = ports.KindCatalogPin
