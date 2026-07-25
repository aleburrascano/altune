package service

import "altune/go-api/internal/discovery/domain"

const SectionCap = 10

type ResultSection struct {
	Kind  domain.ResultKind
	Items []domain.SearchResult
}

type BlendedSlate struct {
	TopResult *domain.SearchResult
	Sections  []ResultSection
}

func BuildBlendedSlate(results []domain.SearchResult) BlendedSlate {
	if len(results) == 0 {
		return BlendedSlate{Sections: []ResultSection{}}
	}

	top := results[0]
	byKind := map[domain.ResultKind][]domain.SearchResult{}
	seen := map[domain.ResultKind]bool{}
	var kindOrder []domain.ResultKind
	for i, r := range results {
		if !seen[r.Kind] {
			seen[r.Kind] = true
			kindOrder = append(kindOrder, r.Kind)
		}
		if i == 0 {
			continue
		}
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}

	sections := make([]ResultSection, 0, len(kindOrder))
	for _, kind := range kindOrder {
		items := byKind[kind]
		if len(items) == 0 {
			continue
		}
		if len(items) > SectionCap {
			items = items[:SectionCap]
		}
		sections = append(sections, ResultSection{Kind: kind, Items: items})
	}

	return BlendedSlate{TopResult: &top, Sections: sections}
}
