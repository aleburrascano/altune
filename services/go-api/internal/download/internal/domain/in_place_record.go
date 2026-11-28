package domain

type InPlaceRecord struct {
	AudioRef           string
	Generation         int
	Verdict            string
	Basis              string
	Score              float64
	BitErrorRate       float64
	Grade              string
	UserUploadMismatch bool
	MayLower           bool
}
