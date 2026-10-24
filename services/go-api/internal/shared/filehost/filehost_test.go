package filehost

import "testing"

func TestParse_ReadsEachHostsFileLink(t *testing.T) {
	cases := []struct{ link, provider, id, page string }{
		{"https://pixeldrain.com/u/ozSpSjn7", "pixeldrain", "ozSpSjn7", "https://pixeldrain.com/u/ozSpSjn7"},
		{"https://www.pixeldrain.com/u/ozSpSjn7", "pixeldrain", "ozSpSjn7", "https://pixeldrain.com/u/ozSpSjn7"},
		{"https://pillows.su/f/4d201f3332e7128f9f148d765244ce98", "pillows", "4d201f3332e7128f9f148d765244ce98", "https://pillows.su/f/4d201f3332e7128f9f148d765244ce98"},
		{"https://pillowcase.su/f/84310b5d2c21df85e4570aec299d1ead", "pillows", "84310b5d2c21df85e4570aec299d1ead", "https://pillows.su/f/84310b5d2c21df85e4570aec299d1ead"},
		{"https://imgur.gg/f/0Q3ydhk", "imgurgg", "0Q3ydhk", "https://imgur.gg/f/0Q3ydhk"},
	}
	for _, c := range cases {
		got, ok := Parse(c.link)
		if !ok || got != (File{Provider: c.provider, ID: c.id, PageURL: c.page}) {
			t.Errorf("Parse(%q) = %+v, %v", c.link, got, ok)
		}
	}
}

func TestParse_RejectsLinksThatAreNotAFile(t *testing.T) {
	for _, link := range []string{
		"https://pixeldrain.com/l/ozSpSjn7",
		"https://pixeldrain.com/u/ab",
		"https://pillows.su/f/short",
		"https://evilpillows.su/f/4d201f3332e7128f9f148d765244ce98",
		"https://imgur.com/f/0Q3ydhk",
		"ftp://imgur.gg/f/0Q3ydhk",
		"https://soundcloud.com/a/b",
	} {
		if got, ok := Parse(link); ok {
			t.Errorf("Parse(%q) = %+v, want rejected", link, got)
		}
	}
}

func TestDownloadURL_IsBuiltFromAValidIDOnly(t *testing.T) {
	cases := map[[2]string]string{
		{"pixeldrain", "ozSpSjn7"}:                      "https://pixeldrain.com/api/file/ozSpSjn7?download",
		{"pillows", "4d201f3332e7128f9f148d765244ce98"}: "https://api.pillows.su/api/download/4d201f3332e7128f9f148d765244ce98",
	}
	for in, want := range cases {
		if got, ok := DownloadURL(in[0], in[1]); !ok || got != want {
			t.Errorf("DownloadURL(%q, %q) = %q, %v, want %q", in[0], in[1], got, ok, want)
		}
	}
	for _, in := range [][2]string{{"pixeldrain", "../../x"}, {"pillows", "a?b=c"}, {"soundcloud", "x"}} {
		if got, ok := DownloadURL(in[0], in[1]); ok {
			t.Errorf("DownloadURL(%q, %q) = %q, want rejected", in[0], in[1], got)
		}
	}
}

func TestFileOnPage_AcceptsOnlyThisIDsFileOnTheImgurggCDN(t *testing.T) {
	page, ok := FileOnPage("imgurgg", "0Q3ydhk")
	if !ok || page.PageURL != "https://imgur.gg/f/0Q3ydhk" {
		t.Fatalf("FileOnPage = %+v, %v", page, ok)
	}
	body := `<audio src="https://evil.example/0Q3ydhk-x.mp3"></audio><audio src="https://b.imgur.gg/OTHER12-x.mp3"></audio><audio src="https://b.imgur.gg/0Q3ydhk-Got_A_New_One.mp3"></audio>`
	if got := page.File.FindString(body); got != "https://b.imgur.gg/0Q3ydhk-Got_A_New_One.mp3" {
		t.Errorf("file = %q, want only this id's b.imgur.gg file", got)
	}
	if _, ok := DownloadURL("imgurgg", "0Q3ydhk"); ok {
		t.Error("an imgur.gg page must never be handed to the downloader")
	}
	if _, ok := FileOnPage("pixeldrain", "ozSpSjn7"); ok {
		t.Error("pixeldrain has a direct download and no page step")
	}
}
