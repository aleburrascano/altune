package downloadsetup

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/config"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

const deezerPreviewRate = 1.0

func upgradeLane(cfg *config.Config, pool *pgxpool.Pool, ledger download.TrackLedgerPort, svc *download.AcquireTrackAudioService) download.UpgradeLane {
	store := download.NewPostgresUpgradeStore(pool)
	runner := download.NewUpgradeRunner(ledger, svc, store,
		download.UpgradeConfig{
			MaxTries:            cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: cfg.AcquisitionLowBitrateFloorKbps,
		}).
		WithInPlaceCheck(svc, store, cfg.AcquisitionUpgradeVerifyPerHour)
	return download.UpgradeLaneOf(runner, cfg.AcquisitionUpgradeConcurrency)
}

func upgradePreviewMatcher(cfg *config.Config) download.PreviewMatcher {
	printer := download.NewFpcalcPrinter(binpath.Resolve("fpcalc", cfg.FFmpegLocation))
	previews := download.NewDeezerPreview(&http.Client{Timeout: cfg.AcquisitionDeezerPreviewTimeout}, download.DeezerPreviewBaseURL, deezerPreviewRate, printer)
	return download.NewPreviewMatcher(previews, printer, download.DefaultMatchMaxBitErrorRate)
}
