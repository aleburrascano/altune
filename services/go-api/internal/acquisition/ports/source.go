package ports

import "context"

type FindRequest struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration float64
	Identity RecordingIdentity
}

type AudioSource interface {
	Name() string
	Find(ctx context.Context, req FindRequest) ([]AudioCandidate, error)
	Fetch(ctx context.Context, candidate AudioCandidate, outDir string) (filePath string, err error)
}

func SearchQueries(req FindRequest) []string {
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
