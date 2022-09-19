package youtube

import (
	"git.nobrain.org/r4/dischord/extractor"

	"errors"
	"net/url"
)

func init() {
	extractor.AddExtractor("youtube", &Extractor{})
	extractor.AddSearcher("youtube-search", &Searcher{})
	extractor.AddSuggestor("youtube-search-suggestions", &Suggestor{})
}

type matchType int

const (
	matchTypeNone matchType = iota
	matchTypeVideo
	matchTypePlaylist
)

var (
	ErrInvalidInput = errors.New("invalid input")
)

func matches(requireDirectPlaylistUrl bool, input string) matchType {
	u, err := url.Parse(input)
	if err != nil {
		return matchTypeNone
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return matchTypeNone
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return matchTypeNone
	}
	switch u.Host {
	case "www.youtube.com", "youtube.com":
		if u.Path != "/watch" && u.Path != "/playlist" {
			return matchTypeNone
		}
		if q.Has("list") && (!requireDirectPlaylistUrl || u.Path == "/playlist") {
			return matchTypePlaylist
		}
		return matchTypeVideo
	case "youtu.be":
		return matchTypeVideo
	default:
		return matchTypeNone
	}
}

type Extractor struct {
	decryptor decryptor
}

func (e *Extractor) DefaultConfig() extractor.ProviderConfig {
	return extractor.ProviderConfig{
		"require-direct-playlist-url": false,
	}
}

func (e *Extractor) Matches(cfg extractor.ProviderConfig, input string) bool {
	return matches(cfg["require-direct-playlist-url"].(bool), input) != matchTypeNone
}

func (e *Extractor) Extract(cfg extractor.ProviderConfig, input string) ([]extractor.Data, error) {
	switch matches(cfg["require-direct-playlist-url"].(bool), input) {
	case matchTypeVideo:
		d, err := getVideo(&e.decryptor, input)
		if err != nil {
			return nil, err
		}
		return []extractor.Data{d}, nil
	case matchTypePlaylist:
		return getPlaylist(input)
	}
	return nil, ErrInvalidInput
}

type Searcher struct{}

func (s *Searcher) DefaultConfig() extractor.ProviderConfig {
	return extractor.ProviderConfig{}
}

func (s *Searcher) Search(cfg extractor.ProviderConfig, input string) ([]extractor.Data, error) {
	return getSearch(input)
}

type Suggestor struct{}

func (s *Suggestor) DefaultConfig() extractor.ProviderConfig {
	return extractor.ProviderConfig{}
}

func (s *Suggestor) Suggest(cfg extractor.ProviderConfig, input string) ([]string, error) {
	return getSearchSuggestions(input)
}
