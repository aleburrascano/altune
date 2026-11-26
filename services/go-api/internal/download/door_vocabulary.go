package download

import (
	grade "altune/go-api/internal/download/internal/core/audioquality"
	ports "altune/go-api/internal/download/internal/core/ports"
	upgrade "altune/go-api/internal/download/service/replacement"
)

type ClaimNumber = ports.ClaimNumber

type CooldownKind = ports.CooldownKind

type ScratchFolder = ports.ScratchFolder

type (
	InPlaceChecker   = upgrade.InPlaceChecker
	InPlaceRecord    = ports.InPlaceRecord
	InPlaceStore     = ports.InPlaceStore
	ReplaceAttempt   = upgrade.Attempt
	UpgradeAttempt   = ports.UpgradeAttempt
	UpgradeCandidate = ports.UpgradeCandidate
	ReplacementGate  = upgrade.ReplacementGate
	UpgradeRunner    = upgrade.Runner
	UpgradeSelection = ports.UpgradeSelection
)

const PipelineGeneration = grade.PipelineGeneration
