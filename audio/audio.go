package audio

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"fmt"
)

const (
	BufferLength    = 300 // 5min / 3.6MB
	Channels        = 1   // unfortunately, Discord doesn't seem to support stereo at the time
	BitRate         = 96000
	SampleRate      = 48000
	FrameSize       = 960                                      // 960 samples
	FrameDuration   = float64(FrameSize) / float64(SampleRate) // 20ms
	FramesPerSecond = SampleRate / FrameSize
)

var (
	ErrNotHttp = errors.New("the requested resource is not an http/https address")
)

type frameIdx struct {
	start uint
	end   uint
}

// Takes a file path/HTTP(S) stream URL and returns Discord audio frames through
// audioFrameCh. After audioFrameCh is closed, errCh can be read to get any
// potential error. Will cleanly kill ffmpeg if a struct{} is sent through
// killCh (IMPORTANT: only send the kill signal ONCE: there is a chance that
// this goroutine exits just before you send a kill signal; this will be
// absorbed by the channel buffer, but your program might get stuck if you try
// to send two kill signals to a dead stream).
func StreamToDiscordOpus(ffmpegPath, input string, stdin io.Reader, seekSeconds float64, playbackSpeed float64, inetOnly bool) (audioFrameCh <-chan []byte, errCh <-chan error, killCh chan<- struct{}) {
	out := make(chan []byte, BufferLength*FramesPerSecond)
	errch := make(chan error, 1)
	killch := make(chan struct{}, 1)

	go func() {
		defer close(out)
		defer close(errch)

		if inetOnly && !(strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")) {
			errch <- ErrNotHttp
			return
		}

		// Set ffmpeg options
		var cmdOpts []string
		cmdOpts = append(cmdOpts,
			"-vn", // no video
			"-sn", // no subs
			"-dn") // no data encoding
		if seekSeconds != 0.0 {
			cmdOpts = append(cmdOpts,
				"-accurate_seek",
				"-ss", strconv.FormatFloat(seekSeconds, 'f', 5, 64)) // seek duration
		}
		cmdOpts = append(cmdOpts,
			"-i", input)
		if playbackSpeed != 1.0 {
			cmdOpts = append(cmdOpts,
				"-filter:a", "atempo="+strconv.FormatFloat(playbackSpeed, 'f', 5, 64)) // playback speed
		}
		cmdOpts = append(cmdOpts,
			"-ab", strconv.Itoa(BitRate), // audio bit rate
			"-ac", strconv.Itoa(Channels), // audio channels
			"-frame_size", strconv.Itoa(int(FrameDuration*1000)), // frame size (in ms)
			"-f", "opus", // output OPUS audio
			"pipe:1") // output to stdout

		// Prepare ffmpeg command
		cmd := exec.Command(ffmpegPath, cmdOpts...)
		if stdin != nil {
			cmd.Stdin = stdin
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errch <- err
			return
		}

		// Start ffmpeg
		if err := cmd.Start(); err != nil {
			errch <- err
			return
		}

		// We want to let our main loop know if ffmpeg is done
		donech := make(chan error)
		go func() {
			donech <- cmd.Wait()
		}()

		// Ogg decoder
		dec := newOggDecoder(stdout)
		var segDec *oggSegmentDecoder
		startSegDec := false

		// Avoid dropping frames
		wantNewFrame := true

		// Main opus encoder loop
		for {
			var frame []byte

			for wantNewFrame {
				if startSegDec && segDec.More() {
					frame = make([]byte, segDec.SegmentSize())
					if err := segDec.ReadSegment(frame); err != nil {
						errch <- err
						return
					}
					wantNewFrame = false
				} else if dec.More() {
					var err error
					var hdr oggPageHeader
					hdr, segDec, err = dec.Page()
					if err != nil {
						errch <- err
						return
					}
					if hdr.GranulePosition != 0 {
						startSegDec = true
					}
				} else {
					out = nil
					break
				}
			}

			// Channel IO
			select {
			case err := <-donech:
				fmt.Println("Audio done, waiting for read to finish")
				if err != nil {
					// Send error and exit
					errch <- err
					return
				}
				// Process exited normally, wait until all samples are read
				// before closing the channels
				for len(out) != 0 {
					time.Sleep(20 * time.Millisecond)
				}
				fmt.Println("Audio read finished")
				return
			case <-killch:
				// Process was killed by user
				cmd.Process.Signal(os.Interrupt)
				return
			case out <- frame:
				// Output is ready to receive a new frame
				wantNewFrame = true
			}
		}
	}()

	return out, errch, killch
}
