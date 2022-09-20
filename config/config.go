package config

import (
	"github.com/BurntSushi/toml"

	"git.nobrain.org/r4/dischord/extractor"

	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

var (
	ErrTokenNotSet          = errors.New("bot token not set")
	ErrInvalidYoutubeDlPath = errors.New("invalid youtube-dl path")
	ErrInvalidFfmpegPath    = errors.New("invalid FFmpeg path")
	ErrYoutubeDlNotFound    = errors.New("youtube-dl not found, please install it from https://youtube-dl.org/ first")
	ErrFfmpegNotFound       = errors.New("FFmpeg not found, please install it from https://ffmpeg.org first")
	ErrPythonNotInstalled   = errors.New("python not installed")
)

type Config struct {
	Token      string           `toml:"bot-token"`
	FfmpegPath string           `toml:"ffmpeg-path"`
	Extractors extractor.Config `toml:"extractors"`
}

const (
	defaultToken = "insert your Discord bot token here"
)

var (
	ffmpegPaths    = []string{"ffmpeg", "./ffmpeg"}
	youtubeDlPaths = []string{"youtube-dl", "./youtube-dl", "yt-dlp", "./yt-dlp", "youtube-dlc", "./youtube-dlc"}
)

// Returns a valid path if one exists. Returns "" if none found.
func searchExecPaths(paths ...string) string {
	for _, pathbase := range paths {
		path := pathbase
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		if _, err := exec.LookPath(path); err == nil {
			return path
		}
	}
	return ""
}

func write(filename string, cfg *Config) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(`# Insert your Discord bot token here.
# It can be found at https://discord.com/developers/applications -> <your application> -> Bot -> Reset Token.
# Make sure to keep the "" around your token text.
`); err != nil {
		return err
	}
	enc := toml.NewEncoder(file)
	enc.Indent = ""
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return nil
}

func macosEnableExecutable(filename string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	// Commands from http://www.osxexperts.net/
	if runtime.GOARCH == "arm64" {
		cmd := exec.Command("xattr", "-cr", filename)
		if err := cmd.Run(); err != nil {
			return err
		}
		cmd = exec.Command("codesign", "-s", "-", filename)
		if err := cmd.Run(); err != nil {
			return err
		}
	} else if runtime.GOARCH == "amd64" {
		cmd := exec.Command("xattr", "-dr", "com.apple.quarantine", filename)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// Tries to load the given TOML config file. Returns an error if the
// configuration file does not exist or is invalid.
func Load(filename string) (*Config, error) {
	cfg := &Config{}
	_, err := toml.DecodeFile(filename, cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Token == defaultToken || cfg.Token == "" {
		return nil, ErrTokenNotSet
	}
	if err := cfg.Extractors.CheckTypes(); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(cfg.Extractors["youtube-dl"]["youtube-dl-path"].(string)); err != nil {
		return nil, ErrInvalidYoutubeDlPath
	}
	if _, err := exec.LookPath(cfg.FfmpegPath); err != nil {
		return nil, ErrInvalidFfmpegPath
	}
	return cfg, nil
}

// Automatically creates a TOML configuration file with the default values and
// prints information and instructions for the user to stdout.
func Autoconf(filename string) (*Config, error) {
	cfg := &Config{
		Token:      defaultToken,
		Extractors: extractor.DefaultConfig(),
	}

	download := func(executable bool, urlsByOS map[string]map[string]string) (filename string, err error) {
		filename, err = download(executable, urlsByOS, func(progress float32) {
			fmt.Printf("Progress: %.1f%%\r", progress*100.0)
		})
		if err != nil {
			fmt.Println()
			return "", err
		} else {
			fmt.Println("Progress: Finished downloading")
		}
		return filename, nil
	}

	python3IsPython := false
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("python"); err != nil {
			if _, err := exec.LookPath("python3"); err == nil {
				python3IsPython = true
			} else {
				return nil, ErrPythonNotInstalled
			}
		}
	}

	youtubeDlPath := searchExecPaths(youtubeDlPaths...)
	if youtubeDlPath == "" {
		fmt.Println("Downloading youtube-dl")
		filename, err := download(true, map[string]map[string]string{
			"windows": {
				"amd64": "https://yt-dl.org/downloads/latest/youtube-dl.exe",
				"386":   "https://yt-dl.org/downloads/latest/youtube-dl.exe",
			},
			"any": {
				"any": "https://yt-dl.org/downloads/latest/youtube-dl",
			},
		})
		if err != nil {
			return nil, err
		}
		youtubeDlPath = "./" + filename
		macosEnableExecutable(youtubeDlPath)
		if python3IsPython {
			// Replace first line with `replacement`
			data, err := os.ReadFile(youtubeDlPath)
			if err != nil {
				return nil, err
			}
			replacement := []byte("#!/usr/bin/env python3")
			for i, c := range data {
				if c == '\n' {
					data = append(replacement, data[i:]...)
					break
				}
			}
			if err := os.WriteFile(youtubeDlPath, data, 0777); err != nil {
				return nil, err
			}
		}
	} else {
		fmt.Println("Using youtube-dl executable found at", youtubeDlPath)
	}
	cfg.Extractors["youtube-dl"]["youtube-dl-path"] = youtubeDlPath

	cfg.FfmpegPath = searchExecPaths(ffmpegPaths...)
	if cfg.FfmpegPath == "" {
		targetFile := "ffmpeg"
		if runtime.GOOS == "windows" {
			targetFile += ".exe"
		}
		fmt.Println("Downloading FFmpeg")
		filename, err := download(false, map[string]map[string]string{
			"linux": {
				"amd64": "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz",
				"386":   "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-i686-static.tar.xz",
				"arm64": "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz",
				"arm":   "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-armhf-static.tar.xz",
			},
			"windows": {
				"amd64": "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip",
				"386":   "https://github.com/sudo-nautilus/FFmpeg-Builds-Win32/releases/download/latest/ffmpeg-n5.1-latest-win32-gpl-5.1.zip",
			},
			"darwin": {
				"amd64": "https://evermeet.cx/ffmpeg/getrelease/zip",
				"arm64": "https://www.osxexperts.net/FFmpeg511ARM.zip",
			},
		})
		if err != nil {
			return nil, err
		}
		fmt.Println("Unpacking", targetFile, "from", filename)
		if err := unarchiveSingleFile(filename, targetFile); err != nil {
			return nil, err
		}
		if err := os.Remove(filename); err != nil {
			return nil, err
		}
		cfg.FfmpegPath = "./" + targetFile
		macosEnableExecutable(cfg.FfmpegPath)
	} else {
		fmt.Println("Using FFmpeg executable found at", cfg.FfmpegPath)
	}

	fmt.Println("Writing configuration to", filename)
	write(filename, cfg)
	fmt.Println("Almost done. Now just edit", filename, "and set your bot token.")

	return cfg, nil
}
