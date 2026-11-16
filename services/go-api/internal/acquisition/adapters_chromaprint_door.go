package acquisition

import chromaprint "altune/go-api/internal/acquisition/internal/adapters/chromaprint"

var NewChromaprintIdentifier = chromaprint.NewIdentifier

const ChromaprintMinScore = chromaprint.MinScore
