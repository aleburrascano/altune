package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMusicBrainzAdapter_ReleaseGroupTitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"release-group-count": 2,
			"release-groups": [
				` + mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `,
				` + mbReleaseGroupJSON("rg-2", "Kid A", "Radiohead", "mbid-rh") + `
			]}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
	if err != nil {
		t.Fatalf("ReleaseGroupTitles: %v", err)
	}
	if len(titles) != 2 || titles[0] != "OK Computer" || titles[1] != "Kid A" {
		t.Errorf("titles = %v, want the release-group titles in order", titles)
	}
}
