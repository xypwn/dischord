package ytdl

import (
	"git.nobrain.org/r4/dischord/extractor"

	"strings"
)

func init() {
	extractor.AddExtractor("youtube-dl", &Extractor{})
}

type Extractor struct{}

func (e *Extractor) DefaultConfig() extractor.ProviderConfig {
	return extractor.ProviderConfig{
		"youtube-dl-path": "yt-dlp",
	}
}

func (e *Extractor) Matches(cfg extractor.ProviderConfig, input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

func (e *Extractor) Extract(cfg extractor.ProviderConfig, input string) ([]extractor.Data, error) {
	var res []extractor.Data
	dch, errch := ytdlGet(cfg["youtube-dl-path"].(string), input)
	for v := range dch {
		res = append(res, v)
	}
	for err := range errch {
		return nil, err
	}
	return res, nil
}
