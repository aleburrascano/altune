package downloadsetup

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

func upgradeLane(cfg *config.Config, pool *pgxpool.Pool, dbMetrics library.DBCallMetrics, ledger download.TrackLedgerPort, replace *download.ReplaceService, checker *download.InPlaceChecker) download.UpgradeLane {
	if cfg.AcquisitionUpgradeConcurrency < 1 {
		return download.NoUpgradeLane()
	}
	store := download.NewPostgresUpgradeStore(pool)
	recorder := downloadfromlibrary.NewInPlaceRecorder(library.NewPostgresTrackRepository(pool, dbMetrics), store)
	inPlace, err := download.InPlaceCheckOf(checker, recorder, cfg.AcquisitionUpgradeVerifyPerHour, clock.Now, clock.Sleep)
	if err != nil {
		panic(err)
	}
	runner := download.MustNewUpgradeRunner(ledger, replace, store,
		download.UpgradeConfig{
			MaxTries:            cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: cfg.AcquisitionLowBitrateFloorKbps,
		}, inPlace)
	return download.MustUpgradeLaneOf(runner, cfg.AcquisitionUpgradeConcurrency)
}

func upgradePreviewMatcher(cfg *config.Config, transport http.RoundTripper) download.PreviewMatcher {
	printer := download.NewFpcalcPrinter(binpath.Resolve("fpcalc", cfg.FFmpegLocation))
	previews := download.MustNewDeezerPreview(outbound.MustNewClient(cfg.AcquisitionDeezerPreviewTimeout, transport), download.DeezerPreviewBaseURL, printer, clock.Sleep, clock.Now)
	return download.NewPreviewMatcher(previews, printer, download.DefaultMatchMaxBitErrorRate)
}
