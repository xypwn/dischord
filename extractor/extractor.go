package extractor

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

var (
	ErrNoSearchResults      = errors.New("no search results")
	ErrNoSearchProvider     = errors.New("no search provider available")
	ErrNoSuggestionProvider = errors.New("no search suggestion provider available")
)

var (
	providers     []provider
	extractors    []extractor
	searchers     []searcher
	suggestors    []suggestor
	defaultConfig Config
)

func Extract(cfg Config, input string) ([]Data, error) {
	if err := cfg.CheckValidity(); err != nil {
		return nil, err
	}
	for _, e := range extractors {
		if e.Matches(cfg[e.name], input) {
			data, err := e.Extract(cfg[e.name], input)
			if err != nil {
				return nil, &Error{e.name, err}
			}
			return data, nil
		}
	}
	d, err := Search(cfg, input)
	if err != nil {
		return nil, err
	}
	if len(d) == 0 {
		return nil, ErrNoSearchResults
	}
	return []Data{d[0]}, nil
}

func Search(cfg Config, input string) ([]Data, error) {
	if err := cfg.CheckValidity(); err != nil {
		return nil, err
	}
	for _, s := range searchers {
		data, err := s.Search(cfg[s.name], input)
		if err != nil {
			return nil, &Error{s.name, err}
		}
		return data, nil
	}
	return nil, ErrNoSearchProvider
}

func Suggest(cfg Config, input string) ([]string, error) {
	if err := cfg.CheckValidity(); err != nil {
		return nil, err
	}
	for _, s := range suggestors {
		data, err := s.Suggest(cfg[s.name], input)
		if err != nil {
			return nil, &Error{s.name, err}
		}
		return data, nil
	}
	return nil, ErrNoSuggestionProvider
}

type Error struct {
	ProviderName string
	Err          error
}

func (e *Error) Error() string {
	return "extractor[" + e.ProviderName + "]: " + e.Err.Error()
}

type provider struct {
	Provider
	name string
}

type extractor struct {
	Extractor
	name string
}

type searcher struct {
	Searcher
	name string
}

type suggestor struct {
	Suggestor
	name string
}

type Config map[string]ProviderConfig

func DefaultConfig() Config {
	if defaultConfig == nil {
		cfg := make(Config)
		for _, e := range providers {
			cfg[e.name] = e.DefaultConfig()
		}
		return cfg
	} else {
		return defaultConfig
	}
}

func (cfg Config) CheckValidity() error {
	for chkProvider, chkCfg := range DefaultConfig() {
		if _, ok := cfg[chkProvider]; !ok {
			return fmt.Errorf("extractor config for %v is nil", chkProvider)
		}
		for k, v := range chkCfg {
			expected, got := reflect.TypeOf(v), reflect.TypeOf(cfg[chkProvider][k])
			if got != expected {
				return &ConfigTypeError{
					Provider: chkProvider,
					Key:      k,
					Expected: expected,
					Got:      got,
				}
			}
		}
	}
	return nil
}

type ConfigTypeError struct {
	Provider string
	Key      string
	Expected reflect.Type
	Got      reflect.Type
}

func (e *ConfigTypeError) Error() string {
	expectedName := "nil"
	if e.Expected != nil {
		expectedName = e.Expected.Name()
	}
	gotName := "nil"
	if e.Got != nil {
		gotName = e.Got.Name()
	}
	return "invalid extractor configuration: " + e.Provider + "." + e.Key + ": expected " + expectedName + " but got " + gotName
}

type ProviderConfig map[string]any

type Provider interface {
	DefaultConfig() ProviderConfig
}

type Extractor interface {
	Provider
	Matches(cfg ProviderConfig, input string) bool
	Extract(cfg ProviderConfig, input string) ([]Data, error)
}

func AddExtractor(name string, e Extractor) {
	providers = append(providers, provider{e, name})
	extractors = append(extractors, extractor{e, name})
}

type Searcher interface {
	Provider
	Search(cfg ProviderConfig, input string) ([]Data, error)
}

func AddSearcher(name string, s Searcher) {
	providers = append(providers, provider{s, name})
	searchers = append(searchers, searcher{s, name})
}

type Suggestor interface {
	Provider
	Suggest(cfg ProviderConfig, input string) ([]string, error)
}

func AddSuggestor(name string, s Suggestor) {
	providers = append(providers, provider{s, name})
	suggestors = append(suggestors, suggestor{s, name})
}

type Data struct {
	// Each instance of this struct should be reconstructable by calling
	// Extract() on the SourceUrl
	// String values are "" if not present
	SourceUrl      string
	StreamUrl      string // may expire, see Expires
	Title          string
	PlaylistUrl    string
	PlaylistTitle  string
	Description    string
	Uploader       string
	Duration       int       // in seconds; -1 if unknown
	Expires        time.Time // when StreamUrl expires
	OfficialArtist bool      // only for sites that have non-music (e.g. YouTube); search results only
}
