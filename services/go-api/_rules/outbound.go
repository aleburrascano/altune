package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

func outboundClients(m dsl.Matcher) {
	m.Match(`http.Client{$*_}`, `http.Transport{$*_}`).
		Where(!m.File().PkgPath.Matches(`/platform/outbound(/|$)|test$`)).
		Report("build an HTTP client through outbound.NewClient, the one place that sets its timeout and dial guard")
}
