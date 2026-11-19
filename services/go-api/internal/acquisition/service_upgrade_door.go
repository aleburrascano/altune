package acquisition

import upgrade "altune/go-api/internal/acquisition/service/upgrade"

type (
	UpgradeConfig  = upgrade.Config
	UpgradeFinding = upgrade.Finding
)

var (
	UpgradeInterval   = upgrade.Interval
	NewUpgradePlanner = upgrade.NewPlanner
	NewUpgradeRunner  = upgrade.NewRunner
)
