package discovery

import eval "altune/go-api/internal/discovery/service/eval"

type (
	ArtistIntentReport     = eval.ArtistIntentReport
	ArtistIntentResult     = eval.ArtistIntentResult
	Baselines              = eval.Baselines
	CorrectionReport       = eval.CorrectionReport
	CoverageGap            = eval.CoverageGap
	CoverageReportA        = eval.CoverageReportA
	CoverageReportB        = eval.CoverageReportB
	CoverageSignalAService = eval.CoverageSignalAService
	DetailGolden           = eval.DetailGolden
	DetailItem             = eval.DetailItem
	DetailReport           = eval.DetailReport
	DiversityReport        = eval.DiversityReport
	EvalReport             = eval.EvalReport
	FailureRecord          = eval.FailureRecord
	GateResult             = eval.GateResult
	HarnessReport          = eval.HarnessReport
	HealthReport           = eval.HealthReport
	KnownRecording         = eval.KnownRecording
	LibraryCorpus          = eval.LibraryCorpus
	LibraryEntity          = eval.LibraryEntity
	MergeReport            = eval.MergeReport
	NamedMetric            = eval.NamedMetric
	QueryMode              = eval.QueryMode
	ReplayScore            = eval.ReplayScore
	ResultSummary          = eval.ResultSummary
	Searcher               = eval.Searcher
	VariantSearcher        = eval.VariantSearcher
	VocabularyLookup       = eval.VocabularyLookup
)

var (
	AnyFailed                    = eval.AnyFailed
	BuildBaselines               = eval.BuildBaselines
	BuildRankingCountingFailures = eval.BuildRankingCountingFailures
	IsRecognizedTerm             = eval.IsRecognizedTerm
	LoadBehavioralCorpus         = eval.LoadBehavioralCorpus
	LoadLibraryCorpus            = eval.LoadLibraryCorpus
	EvalMean                     = eval.Mean
	MeasureNoise                 = eval.MeasureNoise
	NewCorpusBuilder             = eval.NewCorpusBuilder
	NewCoverageSignalAService    = eval.NewCoverageSignalAService
	NewCoverageSignalBService    = eval.NewCoverageSignalBService
	NewLibraryCorpus             = eval.NewLibraryCorpus
	ReplayCorpus                 = eval.ReplayCorpus
	RunArtistIntentEval          = eval.RunArtistIntentEval
	RunCorrectionEval            = eval.RunCorrectionEval
	RunDetailEval                = eval.RunDetailEval
	RunDiversityEvalMode         = eval.RunDiversityEvalMode
	RunHealthEval                = eval.RunHealthEval
	RunLibraryEvalMode           = eval.RunLibraryEvalMode
	RunMergeEval                 = eval.RunMergeEval
	SliceFailures                = eval.SliceFailures
	SliceFailuresByPair          = eval.SliceFailuresByPair
	SortedGates                  = eval.SortedGates
	TokenCount                   = eval.TokenCount
	TopBuckets                   = eval.TopBuckets
)

const (
	ArtistIntentAbsent = eval.ArtistIntentAbsent
	ArtistIntentBelowK = eval.ArtistIntentBelowK
	ArtistIntentBuried = eval.ArtistIntentBuried
	ArtistIntentPass   = eval.ArtistIntentPass
	EvalFailNoResults  = eval.EvalFailNoResults
	EvalFailWrongTop   = eval.EvalFailWrongTop
	EvalPass           = eval.EvalPass
	EvalSkipped        = eval.EvalSkipped
	HasIDAttr          = eval.HasIDAttr
	PopBandAttr        = eval.PopBandAttr
	QueryExact         = eval.QueryExact
	QueryTitleOnly     = eval.QueryTitleOnly
	ScriptAttr         = eval.ScriptAttr
	TokenCountAttr     = eval.TokenCountAttr
)
