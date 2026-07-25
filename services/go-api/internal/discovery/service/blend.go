package service

import "altune/go-api/internal/discovery/domain"

const SectionCap = 10

type ResultSection struct {
	Kind    domain.ResultKind
	Items   []domain.SearchResult
	HasMore bool
}

type BlendedSlate struct {
	TopResult *domain.SearchResult
	Sections  []ResultSection
}

func BuildBlendedSlate(page, all []domain.SearchResult) BlendedSlate {
	if len(page) == 0 {
		return BlendedSlate{Sections: []ResultSection{}}
	}

	top := page[0]
	topKey := slateKey(top)

	byKind := map[domain.ResultKind][]domain.SearchResult{}
	countByKind := map[domain.ResultKind]int{}
	seen := map[domain.ResultKind]bool{}
	var kindOrder []domain.ResultKind
	for _, r := range all {
		if !seen[r.Kind] {
			seen[r.Kind] = true
			kindOrder = append(kindOrder, r.Kind)
		}
		if slateKey(r) == topKey {
			continue
		}
		countByKind[r.Kind]++
		if len(byKind[r.Kind]) < SectionCap {
			byKind[r.Kind] = append(byKind[r.Kind], r)
		}
	}

	sections := make([]ResultSection, 0, len(kindOrder))
	for _, kind := range kindOrder {
		if len(byKind[kind]) == 0 {
			continue
		}
		sections = append(sections, ResultSection{
			Kind:    kind,
			Items:   byKind[kind],
			HasMore: countByKind[kind] > SectionCap,
		})
	}

	return BlendedSlate{TopResult: &top, Sections: sections}
}

func slateKey(r domain.SearchResult) string {
	if r.Signature != "" {
		return r.Signature
	}
	return domain.ResultSignature(r)
}
