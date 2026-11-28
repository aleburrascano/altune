package handler

import "github.com/go-chi/chi/v5"

type Routes struct {
	tracks    *TrackHandler
	library   *LibraryHandler
	playlists *PlaylistHandler
	stream    *StreamHandler
	audioURLs *AudioURLHandler
}

func NewRoutes(tracks *TrackHandler, library *LibraryHandler, playlists *PlaylistHandler, stream *StreamHandler, audioURLs *AudioURLHandler) Routes {
	return Routes{tracks: tracks, library: library, playlists: playlists, stream: stream, audioURLs: audioURLs}
}

func (rt Routes) Bounded(r chi.Router) {
	r.Route("/tracks", func(r chi.Router) {
		r.Mount("/", rt.tracks.Routes())
	})
	rt.stream.RecoverRoute(r)
	rt.audioURLs.Routes(r)
	r.Mount("/library", rt.library.Routes())
	r.Mount("/playlists", rt.playlists.Routes())
}

func (rt Routes) LongLived(r chi.Router) {
	rt.stream.StreamRoute(r)
}

func (h *TrackHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.handleListTracks)
	r.With(h.writeLimiter).Post("/", h.handleCreateTrack)
	r.Get("/{trackId}/status", h.handleGetTrackStatus)
	r.Patch("/{trackId}/track-number", h.handleSetTrackNumber)
	r.Delete("/{trackId}", h.handleDeleteTrack)
	h.featuredArtist.Routes(r)
	return r
}

func (h *FeaturedArtistHandler) Routes(r chi.Router) {
	r.Get("/featuring", h.handleListFeaturing)
	r.Post("/featured-backfill", h.handleBackfillFeatured)
}

func (h *LibraryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/albums", h.handleAlbums)
	r.Get("/artists", h.handleArtists)
	return r
}

func (h *PlaylistHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(h.writeLimiter)
		r.Post("/", h.handleCreate)
		r.Post("/{playlistId}/tracks", h.handleAddTrack)
		r.Post("/{playlistId}/tracks/batch", h.handleAddTracks)
	})
	r.Get("/", h.handleList)
	r.Get("/{playlistId}", h.handleGet)
	r.Patch("/{playlistId}", h.handleRename)
	r.Delete("/{playlistId}", h.handleDelete)
	r.Delete("/{playlistId}/tracks/{trackId}", h.handleRemoveTrack)
	r.Delete("/{playlistId}/tracks", h.handleRemoveTracks)
	r.Patch("/{playlistId}/tracks/reorder", h.handleReorder)
	return r
}

func (h *AudioURLHandler) Routes(r chi.Router) {
	r.With(h.limiter).Post("/audio-urls", h.HandleResolve)
}

func (h *StreamHandler) Routes(r chi.Router) {
	h.StreamRoute(r)
	h.RecoverRoute(r)
}

func (h *StreamHandler) StreamRoute(r chi.Router) {
	r.With(h.limiter).Get("/tracks/{trackId}/audio", h.HandleStreamAudio)
}

func (h *StreamHandler) RecoverRoute(r chi.Router) {
	r.With(h.limiter).Post("/tracks/{trackId}/audio/recover", h.HandleRecover)
}
