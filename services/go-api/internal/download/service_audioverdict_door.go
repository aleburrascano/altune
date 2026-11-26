package download

import audiojudge "altune/go-api/internal/download/internal/core/audioverdict"

type (
	JudgeAudio    = audiojudge.Audio
	JudgeCuts     = audiojudge.Cuts
	JudgeIdentity = audiojudge.Identity
	JudgeInput    = audiojudge.Input
	JudgePreview  = audiojudge.Preview
	JudgeSource   = audiojudge.Source
	JudgeVerdict  = audiojudge.Verdict
)

var (
	JudgeCutsWithProvenMax = audiojudge.CutsWithProvenMax
	Judge                  = audiojudge.Judge
)

const (
	JudgeContradicted = audiojudge.Contradicted
	JudgeLikely       = audiojudge.Likely
	JudgeProven       = audiojudge.Proven
)
