package spotify

import (
	"git.nobrain.org/r4/dischord/extractor"
	exutil "git.nobrain.org/r4/dischord/extractor/util"

	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	ErrGettingSessionData       = errors.New("unable to get session data")
	ErrInvalidTrackData         = errors.New("invalid track data")
	ErrTrackNotFound            = errors.New("unable to find track on YouTube")
	ErrUnableToGetYoutubeStream = errors.New("unable to get YouTube stream")
	ErrDecodingApiResponse      = errors.New("error decoding API response")
)

// distance between two integers
func iDist(a, b int) int {
	if a > b {
		return a - b
	} else {
		return b - a
	}
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToUpper(s), strings.ToUpper(substr))
}

type sessionData struct {
	AccessToken                      string `json:"accessToken"`
	AccessTokenExpirationTimestampMs int64  `json:"accessTokenExpirationTimestampMs"`
}

type apiToken struct {
	token   string
	expires time.Time
}

func updateApiToken(token *apiToken) error {
	if time.Now().Before(token.expires) {
		// Token already up-to-date
		return nil
	}

	// Get new token
	var data sessionData
	var funcErr error
	err := exutil.GetHTMLScriptFunc("https://open.spotify.com", false, func(code string) bool {
		if strings.HasPrefix(code, "{\"accessToken\":\"") {
			// Parse session data
			if err := json.Unmarshal([]byte(code), &data); err != nil {
				funcErr = err
				return false
			}
			return false
		}
		return true
	})
	if err != nil {
		return err
	}
	if funcErr != nil {
		return funcErr
	}
	*token = apiToken{
		token:   data.AccessToken,
		expires: time.UnixMilli(data.AccessTokenExpirationTimestampMs),
	}
	return nil
}

type trackData struct {
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	DurationMs   int `json:"duration_ms"`
	ExternalUrls struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Name string `json:"name"`
}

func (d trackData) artistsString() (res string) {
	for i, v := range d.Artists {
		if i != 0 {
			res += ", "
		}
		res += v.Name
	}
	return
}

func (d trackData) titleString() string {
	return d.artistsString() + " - " + d.Name
}

func getTrack(e *Extractor, trackId string) (extractor.Data, error) {
	if err := updateApiToken(&e.token); err != nil {
		return extractor.Data{}, err
	}

	// Make API request for track info
	req, err := http.NewRequest("GET", "https://api.spotify.com/v1/tracks/"+trackId, nil)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+e.token.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return extractor.Data{}, err
	}
	defer resp.Body.Close()

	// Parse track info
	var data trackData
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&data); err != nil {
		return extractor.Data{}, ErrDecodingApiResponse
	}

	if len(data.Artists) == 0 {
		return extractor.Data{}, ErrInvalidTrackData
	}

	// Search for track on YouTube
	results, err := e.ytSearcher.Search(e.ytSearcherConfig, data.Name+" - "+data.artistsString())
	if err != nil {
		return extractor.Data{}, err
	}
	if len(results) == 0 {
		return extractor.Data{}, ErrTrackNotFound
	}

	// Lower is better
	score := func(ytd extractor.Data, resIdx int) (score int) {
		// This function determines the likelihood of a given YouTube video
		// belonging to the Spotify song.
		// It may look pretty complicated, but here's the gist:
		//   - lower scores are better
		//   - the general formula is: resIdx - matchAccuracy / penalty
		//     where 'resIdx' is the position in the search results,
		//     'matchAccuracy' is how well the video superficially matches
		//     with the Spotify song (title, artists, duration) and 'penalty'
		//     measures the hints pointing to the current video being the
		//     wrong one (awfully wrong duration, instrumental version, remix
		//     etc.)
		//   - if the video is from an official artist channel, that makes the
		//     penalty points even more credible, so they're squared
		//   - accuracy and penalty points are multiplicative; this makes them
		//     have exponentially more weight the more they are given

		matchAccuracy := 1.0
		matchPenalty := 1.0
		sqrPenalty := false
		if ytd.OfficialArtist || strings.HasSuffix(ytd.Uploader, " - Topic") {
			matchAccuracy *= 4.0
			sqrPenalty = true
		}
		if containsIgnoreCase(ytd.Title, data.Name) {
			matchAccuracy *= 4.0
		}
		matchingArtists := 0.0
		firstMatches := false
		for i, artist := range data.Artists {
			if containsIgnoreCase(ytd.Uploader, artist.Name) ||
				containsIgnoreCase(ytd.Title, artist.Name) {
				matchingArtists += 1.0
				if i == 0 {
					firstMatches = true
				}
			}
		}
		if firstMatches {
			matchAccuracy *= 2.0
		}
		matchAccuracy *= 2.0 * (matchingArtists / float64(len(data.Artists)))
		durationDist := iDist(ytd.Duration, data.DurationMs/1000)
		if durationDist <= 5 {
			matchAccuracy *= 8.0
		} else if durationDist >= 300 {
			matchPenalty *= 16.0
		}
		spotiArtists := data.artistsString()
		onlyYtTitleContains := func(s string) bool {
			return !containsIgnoreCase(data.Name, s) &&
				!containsIgnoreCase(spotiArtists, s) &&
				containsIgnoreCase(ytd.Title, s)
		}
		if onlyYtTitleContains("instrumental") || onlyYtTitleContains("cover") ||
			onlyYtTitleContains("live") || onlyYtTitleContains("album") {
			matchPenalty *= 8.0
		}
		if onlyYtTitleContains("remix") || onlyYtTitleContains("rmx") {
			matchPenalty *= 8.0
		} else if onlyYtTitleContains("mix") {
			matchPenalty *= 6.0
		}
		if onlyYtTitleContains("vip") {
			matchPenalty *= 6.0
		}
		totalPenalty := matchPenalty
		if sqrPenalty {
			totalPenalty *= totalPenalty
		}
		return resIdx - int(matchAccuracy/totalPenalty)
	}

	// Select the result with the lowest (best) score
	lowestIdx := -1
	lowest := 2147483647
	for i, v := range results {
		score := score(v, i)
		//fmt.Println(i, score, v)
		if score < lowest {
			lowestIdx = i
			lowest = score
		}
	}

	ytData, err := e.ytExtractor.Extract(e.ytExtractorConfig, results[lowestIdx].SourceUrl)
	if err != nil {
		return extractor.Data{}, err
	}

	if len(ytData) != 1 {
		return extractor.Data{}, ErrUnableToGetYoutubeStream
	}

	return extractor.Data{
		SourceUrl: data.ExternalUrls.Spotify,
		StreamUrl: ytData[0].StreamUrl,
		Title:     data.titleString(),
		Uploader:  data.artistsString(),
		Duration:  ytData[0].Duration,
		Expires:   ytData[0].Expires,
	}, nil
}

type playlistData struct {
	ExternalUrls struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Name   string `json:"name"`
	Tracks struct {
		Items []struct {
			Track trackData `json:"track"`
		} `json:"items"`
		Next string `json:"next"`
	} `json:"tracks"`
}

func getPlaylist(e *Extractor, playlistId string) ([]extractor.Data, error) {
	if err := updateApiToken(&e.token); err != nil {
		return nil, err
	}

	var data playlistData
	trackOnlyReq := false
	reqUrl := "https://api.spotify.com/v1/playlists/" + playlistId
	var res []extractor.Data
	for {
		// Make API request for playlist info
		req, err := http.NewRequest("GET", reqUrl, nil)
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Authorization", "Bearer "+e.token.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		// Parse playlist info
		dec := json.NewDecoder(resp.Body)
		if trackOnlyReq {
			// JSON decoder doesn't always overwrite the set value
			data.Tracks.Next = ""
			data.Tracks.Items = nil

			err = dec.Decode(&data.Tracks)
		} else {
			err = dec.Decode(&data)
		}
		if err != nil {
			return nil, ErrDecodingApiResponse
		}

		for _, v := range data.Tracks.Items {
			res = append(res, extractor.Data{
				SourceUrl:     v.Track.ExternalUrls.Spotify,
				Title:         v.Track.titleString(),
				Uploader:      v.Track.artistsString(),
				PlaylistUrl:   data.ExternalUrls.Spotify,
				PlaylistTitle: data.Name,
			})
		}

		if data.Tracks.Next == "" {
			break
		} else {
			reqUrl = data.Tracks.Next
			trackOnlyReq = true
		}
	}
	return res, nil
}

type albumData struct {
	ExternalUrls struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	Name   string `json:"name"`
	Tracks struct {
		Items []trackData `json:"items"`
		Next  string      `json:"next"`
	} `json:"tracks"`
}

func getAlbum(e *Extractor, albumId string) ([]extractor.Data, error) {
	// This function is pretty much copied from getPlaylist, with minor
	// modifications

	if err := updateApiToken(&e.token); err != nil {
		return nil, err
	}

	var data albumData
	trackOnlyReq := false
	reqUrl := "https://api.spotify.com/v1/albums/" + albumId
	var res []extractor.Data
	for {
		// Make API request for album info
		req, err := http.NewRequest("GET", reqUrl, nil)
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Authorization", "Bearer "+e.token.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		// Parse album info
		dec := json.NewDecoder(resp.Body)
		if trackOnlyReq {
			// JSON decoder doesn't always overwrite the set value
			data.Tracks.Next = ""
			data.Tracks.Items = nil

			err = dec.Decode(&data.Tracks)
		} else {
			err = dec.Decode(&data)
		}
		if err != nil {
			return nil, ErrDecodingApiResponse
		}

		for _, v := range data.Tracks.Items {
			res = append(res, extractor.Data{
				SourceUrl:     v.ExternalUrls.Spotify,
				Title:         v.titleString(),
				Uploader:      v.artistsString(),
				PlaylistUrl:   data.ExternalUrls.Spotify,
				PlaylistTitle: data.Name,
			})
		}

		if data.Tracks.Next == "" {
			break
		} else {
			reqUrl = data.Tracks.Next
			trackOnlyReq = true
		}
	}
	return res, nil
}
