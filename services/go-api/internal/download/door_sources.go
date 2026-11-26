package download

import (
	catalogpin "altune/go-api/internal/download/internal/adapters/savedlink"
	domain "altune/go-api/internal/download/internal/core/domain"
	ports "altune/go-api/internal/download/internal/core/ports"
)

type SourceUnavailableError = domain.SourceUnavailableError

var ErrSearchDegraded = domain.ErrSearchDegraded

type SavedLinkSource = catalogpin.Source

const KindCatalogPin = ports.KindCatalogPin
