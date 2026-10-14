package domain

import (
	"reflect"
	"testing"
)

func TestCleanSearchKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		title    string
		artist   string
		uploader string
		want     SearchKeys
	}{
		{
			name: "artist prefix split off", title: "Tate McRae - One track mind", artist: "Tate McRae",
			want: SearchKeys{Artist: "Tate McRae", Title: "One track mind"},
		},
		{
			name: "en dash separator", title: "Tate McRae – One track mind", artist: "Tate McRae",
			want: SearchKeys{Artist: "Tate McRae", Title: "One track mind"},
		},
		{
			name: "pipe separator", title: "Tate McRae | One track mind", artist: "Tate McRae",
			want: SearchKeys{Artist: "Tate McRae", Title: "One track mind"},
		},
		{
			name: "left side matching only the uploader splits", title: "Tate McRae - One track mind", artist: "", uploader: "Tate McRae VEVO",
			want: SearchKeys{Artist: "Tate McRae", Title: "One track mind", ArtistFromUploader: true},
		},
		{
			name: "left side unrelated to artist is kept", title: "Love - Hate", artist: "Someone Else",
			want: SearchKeys{Artist: "Someone Else", Title: "Love - Hate"},
		},
		{
			name: "star prefix and cjk duplicate dropped", title: "☆ Good Stuff - aespa karina solo (에스파 카리나 솔로)", artist: "aespa karina solo (에스파 카리나 솔로)",
			want: SearchKeys{Artist: "aespa karina solo", Title: "Good Stuff"},
		},
		{
			name: "slowed and reverb", title: "POPTHATRUNK (slowed + reverb)", artist: "POPTHATRUNK",
			want: SearchKeys{Artist: "POPTHATRUNK", Title: "POPTHATRUNK", Qualifiers: []string{"reverb", "slowed"}, Variant: true},
		},
		{
			name: "uploader artist with remix and producer tag", title: "MINDA (민다) NewJeans (뉴진스) - 'Cookie' (Highschool Band Remix) (Prod.MINDA)",
			artist: "MINDA (민다)", uploader: "MINDA (민다)",
			want: SearchKeys{Artist: "MINDA", Title: "MINDA NewJeans - 'Cookie'", Qualifiers: []string{"remix"}, Variant: true},
		},
		{
			name: "extended", title: "Gorah (Extended)", artist: "Gorah",
			want: SearchKeys{Artist: "Gorah", Title: "Gorah", Qualifiers: []string{"extended"}, Variant: true},
		},
		{
			name: "official music video dropped", title: "Never Gonna Give You Up (Official Music Video)", artist: "Rick Astley",
			want: SearchKeys{Artist: "Rick Astley", Title: "Never Gonna Give You Up"},
		},
		{
			name: "remaster is a qualifier but not a variant", title: "Faithfully (2024 Remaster)", artist: "Journey",
			want: SearchKeys{Artist: "Journey", Title: "Faithfully", Qualifiers: []string{"remaster"}},
		},
		{
			name: "remaster dash suffix", title: "Here Comes The Sun - Remastered 2009", artist: "The Beatles",
			want: SearchKeys{Artist: "The Beatles", Title: "Here Comes The Sun", Qualifiers: []string{"remaster"}},
		},
		{
			name: "credit is not a qualifier", title: "Smaxk Or Die (feat. Playboi Carti)", artist: "Ken Carson",
			want: SearchKeys{Artist: "Ken Carson", Title: "Smaxk Or Die"},
		},
		{
			name: "leading paren kept", title: "(I Just) Died In Your Arms", artist: "Cutting Crew",
			want: SearchKeys{Artist: "Cutting Crew", Title: "(I Just) Died In Your Arms"},
		},
		{
			name: "plain title unchanged", title: "Hotel California", artist: "Eagles",
			want: SearchKeys{Artist: "Eagles", Title: "Hotel California"},
		},
		{
			name: "hd tag and prod by dropped", title: "Song Name [HD] (prod by Someone)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song Name"},
		},
		{
			name: "live dash suffix", title: "Song Name - Live", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song Name", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "sped up nightcore and clean", title: "Song (Sped Up) (Nightcore) [Clean]", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"clean", "nightcore", "sped up"}, Variant: true},
		},
		{
			name: "topic artist stripped and flagged", title: "Song", artist: "Some Band - Topic",
			want: SearchKeys{Artist: "Some Band", Title: "Song", ArtistIsChannel: true},
		},
		{
			name: "vevo artist stripped and flagged", title: "Song", artist: "SomeBandVEVO",
			want: SearchKeys{Artist: "SomeBand", Title: "Song", ArtistIsChannel: true},
		},
		{
			name: "title that is only decoration falls back", title: "(Official Video)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "(Official Video)"},
		},
		{
			name: "live dash suffix after artist-containing title", title: "Killer Queen - Live", artist: "Queen",
			want: SearchKeys{Artist: "Queen", Title: "Killer Queen", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "remastered year suffix after artist-containing title", title: "Killer Queen - Remastered 2011", artist: "Queen",
			want: SearchKeys{Artist: "Queen", Title: "Killer Queen", Qualifiers: []string{"remaster"}},
		},
		{
			name: "artist prefix with slowed title stays", title: "Drake - Slowed", artist: "Drake",
			want: SearchKeys{Artist: "Drake", Title: "Slowed"},
		},
		{
			name: "live suffix when title starts with artist name", title: "Love Hurts - Live", artist: "Love",
			want: SearchKeys{Artist: "Love", Title: "Love Hurts", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "title after dash not split on partial artist", title: "Hate - I Love You", artist: "Love",
			want: SearchKeys{Artist: "Love", Title: "Hate - I Love You"},
		},
		{
			name: "artist and live in dash suffix", title: "Wish You Were Here - Pink Floyd Live", artist: "Pink Floyd",
			want: SearchKeys{Artist: "Pink Floyd", Title: "Wish You Were Here", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "katakana remix", title: "Song (リミックス)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"remix"}, Variant: true},
		},
		{
			name: "katakana live", title: "Song (ライブ)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "chinese live", title: "晴天 (现场)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "晴天", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "chinese slowed", title: "晴天 (慢速)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "晴天", Qualifiers: []string{"slowed"}, Variant: true},
		},
		{
			name: "non-latin group without latin neighbour kept", title: "晴天 (晴天)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "晴天 (晴天)"},
		},
		{
			name: "stacked dash suffixes", title: "Song - Slowed Reverb - Remastered", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"remaster", "reverb", "slowed"}, Variant: true},
		},
		{
			name: "year remaster suffix", title: "Pictures of You - 2010 Remaster", artist: "The Cure",
			want: SearchKeys{Artist: "The Cure", Title: "Pictures of You", Qualifiers: []string{"remaster"}},
		},
		{
			name: "live suffix", title: "Mr. Brightside - Live", artist: "The Killers",
			want: SearchKeys{Artist: "The Killers", Title: "Mr. Brightside", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "hyphenated sped-up", title: "Song (sped-up)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"sped up"}, Variant: true},
		},
		{
			name: "cover", title: "Song (Cover)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"cover"}, Variant: true},
		},
		{
			name: "mashup", title: "Song (Mashup)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"mashup"}, Variant: true},
		},
		{
			name: "bass boosted", title: "Song (Bass Boosted)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"bass boosted"}, Variant: true},
		},
		{
			name: "8d audio", title: "Song (8D Audio)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"8d audio"}, Variant: true},
		},
		{
			name: "remixed", title: "Song (Remixed)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"remix"}, Variant: true},
		},
		{
			name: "edit", title: "Song (Edit)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"edit"}, Variant: true},
		},
		{
			name: "unplugged", title: "Song (Unplugged)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"unplugged"}, Variant: true},
		},
		{
			name: "official live video", title: "Song (Official Live Video)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"live"}, Variant: true},
		},
		{
			name: "radio edit is not also edit", title: "Song (Radio Edit)", artist: "A",
			want: SearchKeys{Artist: "A", Title: "Song", Qualifiers: []string{"radio edit"}, Variant: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CleanSearchKeys(tt.title, tt.artist, tt.uploader)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CleanSearchKeys(%q, %q, %q) = %+v, want %+v", tt.title, tt.artist, tt.uploader, got, tt.want)
			}
		})
	}
}
