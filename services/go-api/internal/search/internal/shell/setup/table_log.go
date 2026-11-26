package setup

import (
	"context"
	"log/slog"
	"strings"
)

func (t *providerTable) log(ctx context.Context, logger *slog.Logger) {
	for _, p := range t.providers {
		logger.InfoContext(ctx, "discovery.provider", p.logAttrs()...)
	}
}

func (p provider) logAttrs() []any {
	if p.offReason != "" {
		return []any{"name", string(p.name), "on", false, "reason", p.offReason}
	}
	return []any{"name", string(p.name), "on", true, "jobs", joinJobs(p.jobs())}
}

func joinJobs(jobs []job) string {
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = string(j)
	}
	return strings.Join(names, ",")
}
