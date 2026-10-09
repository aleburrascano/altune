package ytdlp

import "altune/go-api/internal/acquisition/ports"

func searchQueries(req ports.FindRequest) []string {
	var queries []string
	if req.ISRC != "" {
		queries = append(queries, req.ISRC)
	}
	queries = append(queries, req.Title+" "+req.Artist)
	if req.Album != "" {
		queries = append(queries, req.Title+" "+req.Artist+" "+req.Album)
	}
	queries = append(queries, req.Title+" "+req.Artist+" audio")
	return queries
}
