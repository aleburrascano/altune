package download

import upgrade "altune/go-api/internal/download/service/replacement"

type (
	UpgradeConfig  = upgrade.Config
	UpgradeFinding = upgrade.Finding
)

var (
	UpgradeInterval       = upgrade.Interval
	NewReplacementPlanner = upgrade.NewReplacementPlanner
	NewUpgradeRunner      = upgrade.NewRunner
)
