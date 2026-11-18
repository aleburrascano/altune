package catalogtest

import memory "altune/go-api/internal/catalog/internal/adapters/memory"

var (
	AssertAttrs      = memory.AssertAttrs
	CaptureAuditLogs = memory.CaptureAuditLogs
)
