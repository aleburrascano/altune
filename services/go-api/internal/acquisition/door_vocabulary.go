package acquisition

import (
	grade "altune/go-api/internal/acquisition/internal/core/grade"
	ports "altune/go-api/internal/acquisition/internal/core/ports"
	upgrade "altune/go-api/internal/acquisition/service/upgrade"
)

type Fence = ports.Fence

type CooldownKind = ports.CooldownKind

type TempWorkspace = ports.TempWorkspace

type (
	InPlaceChecker   = upgrade.InPlaceChecker
	InPlaceRecord    = ports.InPlaceRecord
	InPlaceStore     = ports.InPlaceStore
	ReplaceAttempt   = upgrade.Attempt
	UpgradeAttempt   = ports.UpgradeAttempt
	UpgradeCandidate = ports.UpgradeCandidate
	UpgradeGate      = upgrade.Gate
	UpgradeRunner    = upgrade.Runner
	UpgradeSelection = ports.UpgradeSelection
)

const PipelineGeneration = grade.PipelineGeneration
