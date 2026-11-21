package ranking

import (
	"context"
	"log/slog"
	"math"
)

func logEvaluated(ctx context.Context, track Track, e entry) {
	c := e.candidate
	slog.InfoContext(ctx, "candidate_evaluated",
		"track_id", track.ID,
		"source", c.Source,
		"candidate_title", c.Title,
		"candidate_channel", c.Channel,
		"candidate_duration", c.Duration,
		"candidate_views", c.ViewCount,
		"identity_score", math.Round(e.ident*10)/10,
		"metadata_rank", math.Round(e.meta*1000)/1000,
		"qualifier_distance", e.qualDistance,
		"is_topic", e.uploader == ChannelTopic,
		"artist_match", e.artistMatch,
		"feature_match", e.featMatch,
		"explicit", c.Explicit,
		"track_artist", track.Artist,
	)
}
