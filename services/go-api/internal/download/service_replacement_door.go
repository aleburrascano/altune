package download

import (
	"altune/go-api/internal/download/internal/core/ports"
	upgrade "altune/go-api/internal/download/service/replacement"
)

type (
	UpgradeConfig    = upgrade.Config
	UpgradeFinding   = upgrade.Finding
	UpgradeInPlace   = upgrade.InPlace
	UpgradeReplacer  = upgrade.Replacer
	UpgradeStorePort = ports.UpgradeStore
)

var (
	UpgradeInterval       = upgrade.Interval
	NewReplacementPlanner = upgrade.NewReplacementPlanner
	NewUpgradeRunner      = upgrade.NewRunner
	MustNewUpgradeRunner  = upgrade.MustNewRunner
	NoInPlaceCheck        = upgrade.NoInPlaceCheck
	InPlaceCheckOf        = upgrade.InPlaceCheckOf
)

var (
	ErrMissingRunnerPort  = upgrade.ErrMissingRunnerPort
	ErrMissingInPlacePort = upgrade.ErrMissingInPlacePort
)
