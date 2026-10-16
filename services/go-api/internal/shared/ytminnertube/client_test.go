package ytminnertube

import (
	"encoding/json"
	"testing"
)

func TestBody_IsTheWebRemixContext(t *testing.T) {
	raw, err := json.Marshal(Body(map[string]any{"query": "q", "params": "p"}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"context":{"client":{"clientName":"WEB_REMIX","clientVersion":"1.20220715.04.00","gl":"US","hl":"en"},"user":{"lockedSafetyMode":false}},"params":"p","query":"q"}`
	if string(raw) != want {
		t.Fatalf("body = %s, want %s", raw, want)
	}
}

func TestURL_CarriesTheEndpointAndKey(t *testing.T) {
	got := URL("https://music.youtube.com", "search")
	want := "https://music.youtube.com/youtubei/v1/search?key=AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"
	if got != want {
		t.Fatalf("url = %s, want %s", got, want)
	}
}
