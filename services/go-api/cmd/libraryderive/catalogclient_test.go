package main

import (
	"testing"
	"time"
)

func TestCatalogClient_WaitStaysBetweenTheBaseBackoffAndTheMaxWaitAtFortyAttempts(t *testing.T) {
	for _, maxWait := range []time.Duration{time.Minute, 0} {
		client := &catalogClient{backoff: retryBackoff, maxWait: maxWait}
		for attempt := 1; attempt <= 40; attempt++ {
			wait := client.waitBefore(attempt)
			if wait < retryBackoff || (maxWait > 0 && wait > maxWait) {
				t.Fatalf("maxWait %v attempt %d: waited %v", maxWait, attempt, wait)
			}
		}
	}
}
