package acquisition

import (
	catalogpin "altune/go-api/internal/acquisition/internal/adapters/catalogpin"
	domain "altune/go-api/internal/acquisition/internal/core/domain"
	ports "altune/go-api/internal/acquisition/internal/core/ports"
)

type SourceUnavailableError = domain.SourceUnavailableError

var ErrSearchDegraded = domain.ErrSearchDegraded

type CatalogPinSource = catalogpin.Source

const KindCatalogPin = ports.KindCatalogPin
