package fansheets_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func releasedRow(title, streaming, link string) []cell {
	return []cell{{value: "Era"}, {value: title + "\n(prod. X)"}, {value: "Single"}, {value: streaming}, {value: "Link", link: link}}
}

func TestReleasedTab_KeepsOnlyOffStreamingSongsWithAPlayableLink(t *testing.T) {
	var rows [][]cell
	for i := range 6 {
		rows = append(rows, releasedRow(fmt.Sprintf("Hit %d", i), "Yes (Spotify)", fmt.Sprintf("https://youtu.be/hit%08d", i)))
	}
	rows = append(rows,
		releasedRow("Do You Want", "No", "https://www.youtube.com/watch?v=JNprs9jO0eU"),
		releasedRow("Old Tape", "No", pillowsLink(7)),
		releasedRow("Lost Video", "No", "https://x.com/lucki/status/1"),
	)
	got := releasedSongs(t, gridBody(t, []string{"Era", "Song", "Type", "On DSPs", "Where"}, rows...))
	if len(got) != 2 {
		t.Fatalf("songs = %+v; want Do You Want and Old Tape", got)
	}
	first := got[0]
	if first.Title != "Do You Want" || !first.OffStreaming || first.AudioLinks[0].Provider != "youtube" || first.AudioLinks[0].ExternalID != "JNprs9jO0eU" {
		t.Errorf("first = %+v, want the YouTube-only Do You Want marked off streaming", first)
	}
	if got[1].AudioLinks[0].Provider != "pillows" {
		t.Errorf("second = %+v, want its pillows link", got[1])
	}
}

func TestReleasedTab_IsUnmappedWithoutAYesNoColumn(t *testing.T) {
	logged := captureLog(t)
	body := gridBody(t, []string{"Name", "Links"}, []cell{{value: "Song"}, {value: "x", link: pillowsLink(1)}})
	got := trackerSongs(t, tabsBody("Unreleased", "Released"), readFixture(t, "ken_carson_unreleased.json"), body)
	for _, s := range got {
		if s.OffStreaming || s.Title == "Song" {
			t.Errorf("song = %+v, want nothing from a released tab without a yes/no column", s)
		}
	}
	if !strings.Contains(logged.String(), "msg=trackers.unmapped_sheet") || !strings.Contains(logged.String(), "tab=Released") {
		t.Errorf("log = %q, want the Released tab reported as unmapped", logged.String())
	}
}

func TestReleasedTab_OnlyTheTabNamedReleasedIsRead(t *testing.T) {
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.tabs = tabsBody("Released [old]", "💿 Unreleased", "📻 Released")
	if _, err := srv.client(nil).ArtistSongs(context.Background(), "Ken Carson"); err != nil {
		t.Fatal(err)
	}
	if got := srv.ranges(); !slices.Contains(got, quotedTab("📻 Released")) || slices.Contains(got, quotedTab("Released [old]")) {
		t.Errorf("grid reads = %q, want the live 📻 Released tab and not Released [old]", got)
	}

	srv = newSheetsServer(t, readFixture(t, "hub_artists.json"), readFixture(t, "ken_carson_unreleased.json"))
	srv.tabs = tabsBody("Copy of Released", "Unreleased")
	if _, err := srv.client(nil).ArtistSongs(context.Background(), "Ken Carson"); err != nil {
		t.Fatal(err)
	}
	if got := srv.ranges(); !slices.Equal(got, []string{quotedTab("Unreleased")}) {
		t.Errorf("grid reads = %q, want only the Unreleased tab", got)
	}
}
