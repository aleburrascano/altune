package eval

type CandidateRanking map[string][]string

type ReplayScore struct {
	Positives     int
	Negatives     int
	Found         int
	MRR           float64
	NegativeLeakK int
	TopK          int
}

func rankOf(order []string, sig string) int {
	for i, s := range order {
		if s == sig {
			return i
		}
	}
	return -1
}

func ReplayCorpus(corpus BehavioralCorpus, ranking CandidateRanking, topK int) ReplayScore {
	score := ReplayScore{TopK: topK}
	var rrSum float64
	for _, e := range corpus.Entries {
		order := ranking[e.Query]
		idx := rankOf(order, e.ResultSignature)
		if e.Polarity > 0 {
			score.Positives++
			if idx >= 0 {
				score.Found++
				rrSum += 1.0 / float64(idx+1)
			}
			continue
		}
		score.Negatives++
		if idx >= 0 && idx < topK {
			score.NegativeLeakK++
		}
	}
	if score.Positives > 0 {
		score.MRR = rrSum / float64(score.Positives)
	}
	return score
}
