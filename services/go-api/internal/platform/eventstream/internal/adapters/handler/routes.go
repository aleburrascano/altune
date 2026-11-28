package handler

import "github.com/go-chi/chi/v5"

type Routes struct {
	stream *EventStreamHandler
}

func NewRoutes(stream *EventStreamHandler) Routes {
	return Routes{stream: stream}
}

func (rt Routes) LongLived(r chi.Router) {
	r.Handle("/events", rt.stream)
}
