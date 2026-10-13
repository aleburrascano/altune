package shared

type AcquisitionProvenance string

const (
	ProvenanceVerified     AcquisitionProvenance = "verified"
	ProvenanceCorroborated AcquisitionProvenance = "corroborated"
	ProvenanceBestEffort   AcquisitionProvenance = "best_effort"
)

func (p AcquisitionProvenance) Valid() bool {
	switch p {
	case ProvenanceVerified, ProvenanceCorroborated, ProvenanceBestEffort:
		return true
	}
	return false
}
