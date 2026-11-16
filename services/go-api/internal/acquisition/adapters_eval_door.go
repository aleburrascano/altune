package acquisition

import eval "altune/go-api/internal/acquisition/internal/adapters/eval"

type (
	EvalCandidate     = eval.Candidate
	EvalCase          = eval.Case
	EvalCatalogSource = eval.CatalogSource
	EvalSource        = eval.Source
	EvalTrack         = eval.Track
)

var (
	LoadEvalBaseline      = eval.LoadBaseline
	LoadEvalDir           = eval.LoadDir
	LoadEmbeddedEvalCases = eval.LoadEmbedded
	RunAllEvalCases       = eval.RunAll
	SummarizeEval         = eval.Summarize
)
