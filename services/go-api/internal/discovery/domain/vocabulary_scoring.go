package domain

// Vocabulary fuzzy-match weights — the policy for "how similar is similar
// enough" when ranking suggestion/correction candidates. This is domain policy
// (a tuning decision about the model), not Redis-adapter mechanics: it lived in
// the vocabulary cache adapter, untestable without Redis. Lifted here so it has
// a home with the model it serves and can be exercised as a pure function.
const (
	weightJaccard     = 0.35
	weightLevenshtein = 0.30
	weightPhonetic    = 0.20
	weightLengthSim   = 0.15
)

// VocabularyMatchScore combines the four similarity signals — each expected in
// [0,1] — into a single ranking score with the tuned weights. The adapter
// computes the signals (trigram Jaccard, Levenshtein similarity, phonetic match,
// length similarity) from its candidate set; this function owns how they trade
// off against each other.
func VocabularyMatchScore(jaccard, levenshteinSim, phonetic, lengthSim float64) float64 {
	return weightJaccard*jaccard +
		weightLevenshtein*levenshteinSim +
		weightPhonetic*phonetic +
		weightLengthSim*lengthSim
}
