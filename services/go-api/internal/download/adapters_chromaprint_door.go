package download

import chromaprint "altune/go-api/internal/download/internal/adapters/chromaprint"

var (
	NewChromaprintIdentifier     = chromaprint.NewIdentifier
	MustNewChromaprintIdentifier = chromaprint.MustNewIdentifier
)

var (
	ErrChromaprintClientRequired = chromaprint.ErrClientRequired
)

const ChromaprintMinScore = chromaprint.MinScore
