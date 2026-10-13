package discoverytest

import "sync"

type RecordingActivityFeed struct {
	Mu     sync.Mutex
	Events []string
}

func (r *RecordingActivityFeed) EmitActivity(eventType string) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	r.Events = append(r.Events, eventType)
}

func (r *RecordingActivityFeed) Recorded() []string {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	return append([]string(nil), r.Events...)
}
