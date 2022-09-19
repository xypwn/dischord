package ytdl

import (
	"git.nobrain.org/r4/dischord/extractor"

	"bufio"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrUnsupportedUrl = errors.New("unsupported URL")
)

// A very reduced version of the JSON structure returned by youtube-dl
type ytdlMetadata struct {
	Title       string  `json:"title"`
	Extractor   string  `json:"extractor"`
	Duration    float32 `json:"duration"`
	WebpageUrl  string  `json:"webpage_url"`
	Playlist    string  `json:"playlist"`
	Uploader    string  `json:"uploader"`
	Description string  `json:"description"`
	Formats     []struct {
		Url    string `json:"url"`
		Format string `json"format"`
		VCodec string `json:"vcodec"`
	} `json:"formats"`
}

// Gradually sends all audio URLs through the string channel. If an error occurs, it is sent through the
// error channel. Both channels are closed after either an error occurs or all URLs have been output.
func ytdlGet(youtubeDLPath, input string) (<-chan extractor.Data, <-chan error) {
	out := make(chan extractor.Data)
	errch := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errch)

		// Set youtube-dl args
		var ytdlArgs []string
		ytdlArgs = append(ytdlArgs, "-j", input)

		// Prepare command for execution
		cmd := exec.Command(youtubeDLPath, ytdlArgs...)
		cmd.Env = []string{"LC_ALL=en_US.UTF-8"} // Youtube-dl doesn't recognize some chars if LC_ALL=C or not set at all
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errch <- err
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			errch <- err
			return
		}

		// Catch any errors put out by youtube-dl
		stderrReadDoneCh := make(chan struct{})
		var ytdlError string
		go func() {
			sc := bufio.NewScanner(stderr)
			for sc.Scan() {
				line := sc.Text()
				if strings.HasPrefix(line, "ERROR: ") {
					ytdlError = strings.TrimPrefix(line, "ERROR: ")
				}
			}
			stderrReadDoneCh <- struct{}{}
		}()

		// Start youtube-dl
		if err := cmd.Start(); err != nil {
			errch <- err
			return
		}

		// We want to let our main loop know when youtube-dl is done
		donech := make(chan error)
		go func() {
			donech <- cmd.Wait()
		}()

		// Main JSON decoder loop
		dec := json.NewDecoder(stdout)
		for dec.More() {
			// Read JSON
			var m ytdlMetadata
			if err := dec.Decode(&m); err != nil {
				errch <- err
				return
			}

			// Extract URL from metadata (the latter formats are always the better with youtube-dl)
			for i := len(m.Formats) - 1; i >= 0; i-- {
				format := m.Formats[i]
				if format.VCodec == "none" {
					out <- extractor.Data{
						SourceUrl:     m.WebpageUrl,
						StreamUrl:     format.Url,
						Title:         m.Title,
						PlaylistTitle: m.Playlist,
						Description:   m.Description,
						Uploader:      m.Uploader,
						Duration:      int(m.Duration),
						Expires:       time.Now().Add(10 * 365 * 24 * time.Hour),
					}
					break
				}
			}
		}

		// Wait for command to finish executing and catch any errors
		err = <-donech
		<-stderrReadDoneCh
		if err != nil {
			if ytdlError == "" {
				errch <- err
			} else {
				if strings.HasPrefix(ytdlError, "Unsupported URL: ") {
					errch <- ErrUnsupportedUrl
				} else {
					errch <- errors.New("ytdl: " + ytdlError)
				}
			}
			return
		}
	}()

	return out, errch
}
