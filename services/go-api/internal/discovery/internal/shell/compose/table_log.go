package compose

import (
	"context"
	"log/slog"
	"strings"
)

func (t *providerTable) log(ctx context.Context, logger *slog.Logger) {
	for _, p := range t.providers {
		logger.InfoContext(ctx, "discovery.provider", p.logAttrs()...)
	}
	for _, entry := range t.unknownAllowlisted {
		logger.WarnContext(ctx, "discovery.search_allowlist_unknown", "entry", entry, "reason", "names no search provider")
	}
}

func (p provider) logAttrs() []any {
	if p.offReason != "" {
		return []any{"name", string(p.name), "on", false, "reason", p.offReason}
	}
	attrs := []any{"name", string(p.name), "on", true, "jobs", joinJobs(p.jobs())}
	if p.searchOffReason != "" {
		attrs = append(attrs, "search", false, "reason", p.searchOffReason)
	}
	return attrs
}

func joinJobs(jobs []job) string {
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = string(j)
	}
	return strings.Join(names, ",")
}
