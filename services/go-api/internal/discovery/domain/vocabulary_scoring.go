package domain

const (
	weightJaccard     = 0.35
	weightLevenshtein = 0.30
	weightPhonetic    = 0.20
	weightLengthSim   = 0.15
)

func VocabularyMatchScore(jaccard, levenshteinSim, phonetic, lengthSim float64) float64 {
	return weightJaccard*jaccard +
		weightLevenshtein*levenshteinSim +
		weightPhonetic*phonetic +
		weightLengthSim*lengthSim
}
