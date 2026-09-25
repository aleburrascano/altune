package requeststore

// Trace sizes are estimates charged to the store's byte budget: the length of
// every string a trace holds plus a fixed per-element overhead standing in for
// struct headers and numeric fields, so even rows of empty strings cost memory.
const (
	stringHeaderBytes = 16
	providerRowBytes  = 128
	resultRowBytes    = 176
	detailRowBytes    = 48
	detailHeaderBytes = 96
	exchangeRowBytes  = 128
)

// exchangeSize charges every string an exchange holds, not only its response
// body: a body-less exchange still costs the URL, method and error text it
// keeps alive, and the store's budget is what stands between those and the heap.
func exchangeSize(ex Exchange) int {
	return exchangeRowBytes + len(ex.Method) + len(ex.URL) + len(ex.RespBody) + len(ex.Err)
}

func searchTraceSize(query string, kinds []string, user string, providers []ProviderTrace, final []ResultRow) int {
	return len(query) + len(user) + stringsSize(kinds) + providersSize(providers) + resultRowsSize(final)
}

func providersSize(providers []ProviderTrace) int {
	n := 0
	for _, p := range providers {
		n += providerRowBytes + len(p.Provider) + len(p.Status) + len(p.Err) + resultRowsSize(p.Results)
	}
	return n
}

func resultRowsSize(rows []ResultRow) int {
	n := 0
	for _, r := range rows {
		n += resultRowBytes + len(r.Kind) + len(r.Title) + len(r.Subtitle) + len(r.ImageURL) +
			stringsSize(r.Sources) + len(r.ArtworkSource) + len(r.ArtworkResolutionPath) +
			len(r.ResolutionTier) + len(r.Confidence)
	}
	return n
}

func detailTraceSize(d *DetailTrace) int {
	n := detailHeaderBytes + len(d.Kind) + len(d.Provider) + len(d.Artist) + len(d.Status)
	for _, it := range d.Items {
		n += detailRowBytes + len(it.Title) + len(it.ConsensusVerdict)
	}
	return n
}

func stringsSize(ss []string) int {
	n := 0
	for _, s := range ss {
		n += stringHeaderBytes + len(s)
	}
	return n
}
