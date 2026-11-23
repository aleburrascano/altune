package app_test

import (
	"strings"
	"testing"
)

func TestAcquisitionUpgrade_StartsWithAnAcquisitionSource(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.YtMusicEnabled = true
	cfg.AcquisitionUpgradePerHour, cfg.AcquisitionUpgradeConcurrency = 30, 1

	if logged := assembleLogging(t, cfg).logs.String(); !strings.Contains(logged, `msg="acquisition upgrade started"`) {
		t.Errorf("the acquisition upgrade did not start although a source wires the scheduler:\n%s", logged)
	}
}

func TestAcquisitionUpgrade_NoSchedulerStartsNoUpgrade(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.AcquisitionUpgradePerHour = 30

	if logged := assembleLogging(t, cfg).logs.String(); strings.Contains(logged, "acquisition upgrade started") {
		t.Errorf("the acquisition upgrade started without a scheduler to upgrade through:\n%s", logged)
	}
}
