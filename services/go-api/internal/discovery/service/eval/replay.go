package eval

// Offline counterfactual replay.
//
// Score a candidate ranker over historical behavioral labels WITHOUT serving it:
// for each labelled (query, result_signature), find the rank the candidate gives
// that result. Positives (completed / library_add) should rank high; negatives
// (wrong_album) should not leak into the top. This collapses an experiment from
// weeks-shipped-dark to a same-day offline run.
//
// The candidate's behavior is supplied as a CandidateRanking — query → ordered
// result_signatures — produced offline by replaying the candidate ranker over
// the corpus queries (or by reconstructing the served order from impression
// logs). Keeping the scorer pure over that map makes it deterministic and
// testable, independent of how the ordering was obtained.

// CandidateRanking maps a query to the ordered result_signatures a candidate
// ranker would serve.
type CandidateRanking map[string][]string

// ReplayScore is the outcome of scoring a candidate against the corpus.
type ReplayScore struct {
	Positives     int     // positive labels scored
	Negatives     int     // negative labels scored
	Found         int     // positives the candidate ranked at all
	MRR           float64 // mean reciprocal rank of positive labels (0 = unranked)
	NegativeLeakK int     // negatives appearing within the top-K
	TopK          int
}

// rankOf returns the 0-based position of sig in order, or -1 if absent.
func rankOf(order []string, sig string) int {
	for i, s := range order {
		if s == sig {
			return i
		}
	}
	return -1
}

// ReplayCorpus scores a candidate ranking against the behavioral corpus. MRR is
// averaged over positive labels (a positive the candidate never ranks contributes
// 0); NegativeLeakK counts negative labels the candidate places within topK.
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
