package download

import (
	upgrade "altune/go-api/internal/download/service/replacement"
)

type UpgradeConfig = upgrade.Config

var (
	UpgradeInterval       = upgrade.Interval
	NewReplacementPlanner = upgrade.NewReplacementPlanner
	MustNewUpgradeRunner  = upgrade.MustNewRunner
	InPlaceCheckOf        = upgrade.InPlaceCheckOf
)
