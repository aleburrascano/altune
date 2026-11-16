package trackers

import (
	"fmt"
	"testing"
)

func releasedRow(title, streaming, link string) []gridCell {
	return []gridCell{{FormattedValue: "Era"}, {FormattedValue: title + "\n(prod. X)"}, {FormattedValue: "Single"}, {FormattedValue: streaming}, {FormattedValue: "Link", Hyperlink: link}}
}

func TestParseReleased_KeepsOnlyOffStreamingSongsWithAPlayableLink(t *testing.T) {
	var rows [][]gridCell
	for i := range 6 {
		rows = append(rows, releasedRow(fmt.Sprintf("Hit %d", i), "Yes (Spotify)", fmt.Sprintf("https://youtu.be/hit%08d", i)))
	}
	rows = append(rows,
		releasedRow("Do You Want", "No", "https://www.youtube.com/watch?v=JNprs9jO0eU"),
		releasedRow("Old Tape", "No", pillowsLink(7)),
		releasedRow("Lost Video", "No", "https://x.com/lucki/status/1"),
	)
	got, mapped, err := parseReleased(gridBody(t, gridOf([]string{"Era", "Song", "Type", "On DSPs", "Where"}, rows...)))
	if err != nil || !mapped || len(got) != 2 {
		t.Fatalf("parseReleased = %+v, mapped %v, err %v; want Do You Want and Old Tape", got, mapped, err)
	}
	if got[0].title != "Do You Want" || !got[0].offStreaming || got[0].links[0].Provider != "youtube" || got[0].links[0].ExternalID != "JNprs9jO0eU" {
		t.Errorf("first = %+v, want the YouTube-only Do You Want marked off streaming", got[0])
	}
	if got[1].links[0].Provider != "pillows" {
		t.Errorf("second = %+v, want its pillows link", got[1])
	}
}

func TestParseReleased_IsUnmappedWithoutAYesNoColumn(t *testing.T) {
	grid := gridOf([]string{"Name", "Links"}, []gridCell{{FormattedValue: "Song"}, {FormattedValue: "x", Hyperlink: pillowsLink(1)}})
	if rows, mapped, err := parseReleased(gridBody(t, grid)); err != nil || mapped || len(rows) != 0 {
		t.Errorf("parseReleased = %+v, %v, %v; want unmapped and empty", rows, mapped, err)
	}
}

func TestPickReleased_TakesOnlyTheTabNamedReleased(t *testing.T) {
	if got := pickReleased([]string{"Released [old]", "💿 Unreleased", "📻 Released"}); got != "📻 Released" {
		t.Errorf("pickReleased = %q, want the live 📻 Released tab", got)
	}
	if got := pickReleased([]string{"Copy of Released", "Unreleased"}); got != "" {
		t.Errorf("pickReleased = %q, want none", got)
	}
}
