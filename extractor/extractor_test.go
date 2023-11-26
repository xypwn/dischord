package extractor_test

import (
	"git.nobrain.org/r4/dischord/extractor"
	_ "git.nobrain.org/r4/dischord/extractor/builtins"

	"net/http"
	"net/url"
	"strings"

	"testing"
)

var extractorTestCfg = extractor.DefaultConfig()

func validYtStreamUrl(strmUrl string) bool {
	u, err := url.Parse(strmUrl)
	if err != nil {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	looksOk := u.Scheme == "https" &&
		strings.HasSuffix(u.Host, ".googlevideo.com") &&
		u.Path == "/videoplayback" &&
		q.Has("expire") &&
		q.Has("id")
	if !looksOk {
		return false
	}
	resp, err := http.Get(strmUrl)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func verifySearchResult(t *testing.T, data []extractor.Data, targetUrl string) {
	if len(data) == 0 {
		t.Fatalf("Expected search results but got none")
	}
	first := data[0]
	if first.SourceUrl != targetUrl {
		t.Fatalf("Invalid search result: expected '%v' but got '%v'", targetUrl, first.SourceUrl)
	}
	strmData, err := extractor.Extract(extractorTestCfg, first.SourceUrl)
	if err != nil {
		t.Fatalf("Error retrieving video data: %v", err)
	}
	if len(strmData) != 1 {
		t.Fatalf("Expected exactly one extraction result")
	}
	if !validYtStreamUrl(strmData[0].StreamUrl) {
		t.Fatalf("Invalid YouTube stream URL: got '%v'", strmData[0].StreamUrl)
	}
}

func TestSearch(t *testing.T) {
	extractor.Extract(extractorTestCfg, "https://open.spotify.com/track/22z9GL53FudbuFJqa43Nzj")

	data, err := extractor.Search(extractorTestCfg, "nilered turns water into wine like jesus")
	if err != nil {
		t.Fatalf("Error searching YouTube: %v", err)
	}
	verifySearchResult(t, data, "https://www.youtube.com/watch?v=tAU0FX1d044")
}

func TestSearchPlaylist(t *testing.T) {
	data, err := extractor.Search(extractorTestCfg, "instant regret clicking this playlist epic donut dude")
	if err != nil {
		t.Fatalf("Error searching YouTube: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("Expected search results but got none")
	}
	target := "https://www.youtube.com/playlist?list=PLv3TTBr1W_9tppikBxAE_G6qjWdBljBHJ"
	if data[0].PlaylistUrl != target {
		t.Fatalf("Invalid search result: expected '%v' but got '%v'", target, data[0].SourceUrl)
	}
}

func TestSearchSuggestions(t *testing.T) {
	sug, err := extractor.Suggest(extractorTestCfg, "a")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(sug) == 0 {
		t.Fatalf("Function didn't return any suggestions")
	}
}

func TestSearchIntegrityWeirdCharacters(t *testing.T) {
	data, err := extractor.Extract(extractorTestCfg, "test lol | # !@#%&(*)!&*!äöfáßö®©œæ %% %3 %32")
	if err != nil {
		t.Fatalf("Error searching YouTube: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("Expected exactly one URL but got %v", len(data))
	}
}

func TestYoutubeMusicVideo(t *testing.T) {
	data, err := extractor.Extract(extractorTestCfg, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("Error searching YouTube: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("Expected exactly one URL but got %v", len(data))
	}
	if !validYtStreamUrl(data[0].StreamUrl) {
		t.Fatalf("Invalid YouTube stream URL: got '%v'", data[0].StreamUrl)
	}
}

func TestYoutubeMusicVideoMulti(t *testing.T) {
	for i := 0; i < 10; i++ {
		TestYoutubeMusicVideo(t)
	}
}

func TestYoutubePlaylist(t *testing.T) {
	cfg := extractor.DefaultConfig()
	cfg["youtube"]["require-direct-playlist-url"] = true

	url := "https://www.youtube.com/watch?v=jdUXfsMTv7o&list=PLdImBTpIvHA1xN1Dfw2Ec5NQ5d-LF3ZP5"
	pUrl := "https://www.youtube.com/playlist?list=PLdImBTpIvHA1xN1Dfw2Ec5NQ5d-LF3ZP5"

	data, err := extractor.Extract(cfg, url)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("Expected only a single video")
	}
	if data[0].PlaylistTitle != "" {
		t.Fatalf("Did not expect a playlist")
	}

	data, err = extractor.Extract(cfg, pUrl)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(data) != 14 {
		t.Fatalf("Invalid playlist item count: got '%v'", len(data))
	}

	data, err = extractor.Extract(extractorTestCfg, url)
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(data) != 14 {
		t.Fatalf("Invalid playlist item count: got '%v'", len(data))
	}
	if data[0].Title != "Why I use Linux" {
		t.Fatalf("Invalid title of first item: got '%v'", data[0].Title)
	}
	if data[0].Duration != 70 {
		t.Fatalf("Invalid duration of first item: got '%v'", data[0].Duration)
	}
}

func TestSpotifyTrack(t *testing.T) {
	data, err := extractor.Extract(extractorTestCfg, "https://open.spotify.com/track/7HjaeqTHY6QlwPY0MEjuMF")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("Expected exactly one URL but got %v", len(data))
	}
	if data[0].Title != "Infected Mushroom, Ninet Tayeb - Black Velvet" {
		t.Fatalf("Invalid song title: %v", data[0].Title)
	}
	if data[0].Uploader != "Infected Mushroom, Ninet Tayeb" {
		t.Fatalf("Invalid artists: %v", data[0].Uploader)
	}
	if !validYtStreamUrl(data[0].StreamUrl) {
		t.Fatalf("Invalid YouTube stream URL: got '%v'", data[0].StreamUrl)
	}
}

func TestSpotifyAlbum(t *testing.T) {
	data, err := extractor.Extract(extractorTestCfg, "https://open.spotify.com/album/6YEjK95sgoXQn1yGbYjHsp")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(data) != 11 {
		t.Fatalf("Expected exactly 11 tracks but got %v", len(data))
	}
	if data[0].Title != "Infected Mushroom, Ninet Tayeb - Black Velvet" {
		t.Fatalf("Invalid title of first item: got '%v'", data[0].Title)
	}
	if data[0].Uploader != "Infected Mushroom, Ninet Tayeb" {
		t.Fatalf("Invalid artists in first item: %v", data[0].Uploader)
	}
	if data[1].Title != "Infected Mushroom - While I'm in the Mood" {
		t.Fatalf("Invalid title of second item: got '%v'", data[1].Title)
	}
}

func TestYoutubeDl(t *testing.T) {
	data, err := extractor.Extract(extractorTestCfg, "https://soundcloud.com/pendulum/sets/hold-your-colour-1")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}
	if len(data) != 14 {
		t.Fatalf("Invalid playlist item count: got '%v'", len(data))
	}
	if data[0].Title != "Prelude" {
		t.Fatalf("Invalid title of first item: got '%v'", data[0].Title)
	}
	if data[1].Title != "Slam" {
		t.Fatalf("Invalid title of second item: got '%v'", data[1].Title)
	}
	if data[0].PlaylistTitle != "Hold Your Colour" {
		t.Fatalf("Invalid playlist title: got '%v'", data[0].PlaylistTitle)
	}
}
