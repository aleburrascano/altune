package schema

func (e *Entity) Merge(reference, live Observed) {
	byPath := make(map[string]int, len(e.Fields))
	for i := range e.Fields {
		e.Fields[i].Seen = nil
		byPath[e.Fields[i].Path] = i
	}
	e.mark(reference, SeenReference, byPath)
	e.mark(live, SeenLive, byPath)
	sortFields(e.Fields)
}

func (e *Entity) mark(observed Observed, source string, byPath map[string]int) {
	for path, kind := range observed {
		i, known := byPath[path]
		if !known {
			e.Fields = append(e.Fields, Field{Path: path, Type: kind})
			i = len(e.Fields) - 1
			byPath[path] = i
		}
		e.Fields[i].Seen = append(e.Fields[i].Seen, source)
		if len(e.Fields[i].Seen) == 1 {
			e.Fields[i].Type = kind
		}
	}
}
