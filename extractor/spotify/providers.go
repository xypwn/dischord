package spotify

import (
	"git.nobrain.org/r4/dischord/extractor"
	"git.nobrain.org/r4/dischord/extractor/youtube"

	"errors"
	"net/url"
	"strings"
)

func init() {
	extractor.AddExtractor("spotify", NewExtractor())
}

type matchType int

const (
	matchTypeNone matchType = iota
	matchTypeTrack
	matchTypeAlbum
	matchTypePlaylist
)

var (
	ErrInvalidInput = errors.New("invalid input")
)

func matches(input string) (string, matchType) {
	u, err := url.Parse(input)
	if err != nil {
		return "", matchTypeNone
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", matchTypeNone
	}
	if u.Host != "open.spotify.com" {
		return "", matchTypeNone
	}
	sp := strings.Split(u.Path, "/")
	if len(sp) != 3 || sp[0] != "" {
		return "", matchTypeNone
	}
	switch sp[1] {
	case "track":
		return sp[2], matchTypeTrack
	case "album":
		return sp[2], matchTypeAlbum
	case "playlist":
		return sp[2], matchTypePlaylist
	}
	return "", matchTypeNone
}

type Extractor struct {
	ytSearcher        *youtube.Searcher
	ytSearcherConfig  extractor.ProviderConfig
	ytExtractor       *youtube.Extractor
	ytExtractorConfig extractor.ProviderConfig
	token             apiToken
}

func NewExtractor() *Extractor {
	extractor := &Extractor{}
	extractor.ytSearcher = &youtube.Searcher{}
	extractor.ytSearcherConfig = extractor.ytSearcher.DefaultConfig()
	extractor.ytExtractor = &youtube.Extractor{}
	extractor.ytExtractorConfig = extractor.ytExtractor.DefaultConfig()
	return extractor
}

func (e *Extractor) DefaultConfig() extractor.ProviderConfig {
	return extractor.ProviderConfig{}
}

func (e *Extractor) Matches(cfg extractor.ProviderConfig, input string) bool {
	_, m := matches(input)
	return m != matchTypeNone
}

func (e *Extractor) Extract(cfg extractor.ProviderConfig, input string) ([]extractor.Data, error) {
	id, m := matches(input)
	switch m {
	case matchTypeTrack:
		d, err := getTrack(e, id)
		if err != nil {
			return nil, err
		}
		return []extractor.Data{d}, nil
	case matchTypeAlbum:
		return getAlbum(e, id)
	case matchTypePlaylist:
		return getPlaylist(e, id)
	}
	return nil, ErrInvalidInput
}
