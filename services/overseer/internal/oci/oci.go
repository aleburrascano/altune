package oci

import (
	"altune/overseer/internal/oci/internal/adapters/usage"
	"altune/overseer/internal/oci/internal/core"
)

type (
	Spend           = core.Spend
	SpendLine       = core.SpendLine
	SourceDownError = core.SourceDownError
)

var (
	IsSourceDown             = core.IsSourceDown
	NewFromInstancePrincipal = usage.NewFromInstancePrincipal
)
