package download

import ranking "altune/go-api/internal/download/internal/core/candidateranking"

type (
	RankingRefusal = ranking.Refusal
	RankingStage   = ranking.Stage
	RankingTrack   = ranking.Track
)

var (
	Rank                  = ranking.Rank
	UnrequestedQualifiers = ranking.UnrequestedQualifiers
	UploaderOf            = ranking.UploaderOf
)

const (
	ChannelArtist         = ranking.ChannelArtist
	ChannelOther          = ranking.ChannelOther
	ChannelTopic          = ranking.ChannelTopic
	RankingStageDRM       = ranking.StageDRM
	RankingStageIdentity  = ranking.StageIdentity
	RankingStagePreview   = ranking.StagePreview
	RankingStageQualifier = ranking.StageQualifier
)
