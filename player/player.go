package player

import (
	"git.nobrain.org/r4/dischord/audio"
	"git.nobrain.org/r4/dischord/extractor"

	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Queue struct {
	Done            []extractor.Data
	Playing         *extractor.Data
	Ahead           []extractor.Data
	AheadUnshuffled []extractor.Data
	ShuffleOffset   int
	Paused          bool
	Loop            bool
}

func (q *Queue) Copy() *Queue {
	res := &Queue{
		Done:            append([]extractor.Data{}, q.Done...),
		Playing:         nil,
		Ahead:           append([]extractor.Data{}, q.Ahead...),
		AheadUnshuffled: append([]extractor.Data{}, q.AheadUnshuffled...),
		ShuffleOffset:   q.ShuffleOffset,
		Paused:          q.Paused,
		Loop:            q.Loop,
	}
	if q.Playing != nil {
		res.Playing = &extractor.Data{}
		*res.Playing = *q.Playing
	}
	return res
}

func (q *Queue) InBounds(i int) bool {
	return !(i == 0 && q.Playing == nil) &&
		!(i < 0 && -i-1 >= len(q.Done)) &&
		!(i > 0 && i-1 >= len(q.Ahead))
}

func (q *Queue) At(i int) *extractor.Data {
	if !q.InBounds(i) {
		return nil
	}
	if i < 0 {
		return &q.Done[len(q.Done)+i]
	} else if i == 0 {
		return q.Playing
	} else {
		return &q.Ahead[i-1]
	}
}

// Client commands
// Get commands can most easily be executed via the builtin convenience functions
// NOTE: When using commands like Jump, Swap and Delete, we can't be sure
// that our versions of the indices correspond to the versions the caller
// is referring to, but in this context, that should be fine
type Cmd interface{}
type CmdPlay struct{}
type CmdPause struct{}
type CmdLoop bool
type CmdJump int // relative track to jump to (e.g. -2, -1, 4)
type CmdSkipAll struct{}
type CmdShuffle struct{}
type CmdUnshuffle struct{}
type CmdSwap struct{ A, B int }
type CmdDelete []int
type CmdAddFront []extractor.Data
type CmdAddBack []extractor.Data
type CmdSeek float64  // seconds
type CmdSpeed float64 // speed factor
type CmdPlayFileAndStop struct {
	DoneCh chan<- struct{}
	Data   []byte
}
type CmdGetTime chan<- float64
type CmdGetQueue chan<- *Queue
type CmdGetSpeed chan<- float64

type Client struct {
	CmdCh chan<- Cmd
	// ErrCh is blocking meaning that you will have to constantly read it,
	// either using another goroutine or a select statement whenever sending a
	// command.
	ErrCh <-chan error
}

func (c Client) GetTime() float64 {
	ch := make(chan float64)
	c.CmdCh <- CmdGetTime(ch)
	return <-ch
}

func (c Client) GetQueue() *Queue {
	ch := make(chan *Queue)
	c.CmdCh <- CmdGetQueue(ch)
	return <-ch
}

func (c Client) GetSpeed() float64 {
	ch := make(chan float64)
	c.CmdCh <- CmdGetSpeed(ch)
	return <-ch
}

type Event interface{}
type EventStreamUpdated struct{}
type EventKilled struct{}

type Callback interface{}

// Creates a new player client that will run in parallel and receive commands
// via the returned Client.CmdCh. All audio will be sent via the given outCh.
// Closing the returned Client.CmdCh channel acts as a kill signal.
func NewClient(excfg extractor.Config, ffmpegPath string, outCh chan<- []byte, callbacks ...Callback) Client {
	// Client channels
	cCmdCh := make(chan Cmd)
	cErrCh := make(chan error)

	// Callback setup
	var callbacksStreamUpdated []func(EventStreamUpdated)
	var callbacksKilled []func(EventKilled)
	for _, c := range callbacks {
		switch v := c.(type) {
		case func(EventStreamUpdated):
			callbacksStreamUpdated = append(callbacksStreamUpdated, v)
		case func(EventKilled):
			callbacksKilled = append(callbacksKilled, v)
		default:
			panic("player.NewClient(): invalid callback function type: " + fmt.Sprintf("%T", v))
		}
	}

	go func() {
		nFrames := 0
		tStart := 0.0
		playbackSpeed := 1.0

		var queue Queue

		lastStreamErr := time.Unix(0, 0)

		var filePlaybackDoneCh chan<- struct{}

		// Mostly notes to self:
		// This entire setup is pretty fragile, so I'll try to explain it:
		// Each stream consists of the three fundamental channels below.
		// - audioch sends us the encoded audio frames; if audioch is closed,
		//       that means the stream exited successfully
		// - errch sends any potential error (singular!): if any error occurs,
		//       the client automatically shuts down
		// - killch is where we can send a manual kill signal; no error or
		//       shutdown confirmation will follow
		//
		// Now, it is important here that any time a stream is terminated (either
		// by a user or the stream terminates itself by closing audio ch or
		// sending an error) all three channels are set to nil. Otherwise (and
		// I have spent lots of time debugging this), the goroutine will just
		// lock up trying to send a kill signal through a channel which no
		// longer has a receiver.
		//
		// To achieve a kind of stability, I try to follow the basics rule that:
		// - any time I do manipulate queue.Playing, I kill the current
		//       stream first
		// - there are exactly three ways a stream can end and all have to be
		//       handled separately: termination by the user (only through
		//       calling killStream), exiting with an error (handled in select:
		//       case err, ok := <-errch), and exiting successfully by closing
		//       audioch (handled in select: case cmd, ok := <-cCmdCh: if !ok)
		var audioch <-chan []byte
		var errch <-chan error
		var killch chan<- struct{}

		getPlaybackTime := func() float64 {
			return tStart + float64(nFrames)*audio.FrameDuration*playbackSpeed
		}

		getMaxCachedPlaybackTime := func() float64 {
			return tStart + float64(nFrames+len(audioch))*audio.FrameDuration*playbackSpeed
		}

		readAudioCh := func() <-chan []byte {
			if queue.Paused {
				return nil
			} else {
				return audioch
			}
		}

		killStream := func() {
			if killch != nil {
				killch <- struct{}{}
				audioch = nil
				errch = nil
				killch = nil
			}
		}

		var jumpTracks func(nRel int)

		refreshStream := func(seek float64, speed float64) {
			if queue.Playing == nil {
				// Reset stream info
				nFrames = 0
				tStart = 0.0
				playbackSpeed = 1.0
			} else {
				// Kill the potential current stream
				killStream()

				// Refresh stream URL if necessary
				if queue.Playing.StreamUrl == "" || time.Now().After(queue.Playing.Expires) {
					var data []extractor.Data
					var err error
					for {
						data, err = extractor.Extract(excfg, queue.Playing.SourceUrl)
						if err == nil {
							break
						} else {
							now := time.Now()
							if lastStreamErr.Sub(now) > 5*time.Second {
								lastStreamErr = now
								continue
							} else {
								jumpTracks(1)
								lastStreamErr = now
								cErrCh <- errors.New("skipping stream due to multiple errors")
								break
							}
						}
					}
					if err == nil {
						if len(data) == 1 {
							*queue.Playing = data[0]
						} else {
							cErrCh <- errors.New("got invalid data refreshing stream")
						}
					}
				}

				// Get new stream
				audioch, errch, killch = audio.StreamToDiscordOpus(ffmpegPath, queue.Playing.StreamUrl, nil, seek, speed, true)

				// Reset stream info
				nFrames = 0
				tStart = seek
				playbackSpeed = speed
			}

			for _, c := range callbacksStreamUpdated {
				c(EventStreamUpdated{})
			}
		}

		// Queue overflow safe
		jumpTracks = func(nRel int) {
			// Kill the potential current stream
			killStream()

			if nRel > 0 && nRel > len(queue.Ahead) {
				nRel = len(queue.Ahead)
				if nRel == 0 {
					nRel = 1
				}
			} else if nRel < 0 && -nRel > len(queue.Done) {
				nRel = len(queue.Done)
				if nRel == 0 {
					nRel = -1
				}
			}

			// We can imagine this algorithm like a tape where A B C D E are
			// the items, B is currently playing and we want to skip 2 tracks
			// ahead (D: queue.Done, P: queue.Playing, A: queue.Ahead):
			// A [B] C D E
			// D: A; P: B; A: C D E
			if nRel > 0 {
				// A B [] C D E
				// D: A B, P: , A: C D E
				if queue.Playing != nil {
					queue.Done = append(queue.Done, *queue.Playing)
				}

				// A B C [] D E
				// D: A B C, P: , A: D E
				queue.Done = append(queue.Done, queue.Ahead[:nRel-1]...)
				queue.Ahead = queue.Ahead[nRel-1:]

				// A B C [D] E
				// D: A B C, P: D, A: E
				if len(queue.Ahead) > 0 {
					queue.Playing = new(extractor.Data)
					*queue.Playing = queue.Ahead[0]
					queue.Ahead = queue.Ahead[1:]
				} else {
					queue.Playing = nil
				}

				queue.ShuffleOffset -= nRel
			} else if nRel < 0 {
				// The same thing in reverse

				nRel *= -1

				if queue.Playing != nil {
					queue.Ahead = append([]extractor.Data{*queue.Playing}, queue.Ahead...)
				}

				ql := len(queue.Done)
				queue.Ahead = append(queue.Done[ql-(nRel-1):ql], queue.Ahead...)
				queue.Done = queue.Done[:ql-(nRel-1)]

				if len(queue.Done) > 0 {
					ql := len(queue.Done)
					queue.Playing = new(extractor.Data)
					*queue.Playing = queue.Done[ql-1]
					queue.Done = queue.Done[:ql-1]
				} else {
					queue.Playing = nil
				}

				queue.ShuffleOffset += nRel
			}

			// Update stream
			refreshStream(0, playbackSpeed)
		}

		var unshuffle func()

		shuffle := func() {
			if queue.AheadUnshuffled != nil {
				unshuffle()
			}
			queue.AheadUnshuffled = append([]extractor.Data{}, queue.Ahead...)
			rand.Shuffle(len(queue.Ahead), func(i, j int) {
				queue.Ahead[i], queue.Ahead[j] = queue.Ahead[j], queue.Ahead[i]
			})
			queue.ShuffleOffset = 0
		}

		unshuffle = func() {
			if queue.AheadUnshuffled == nil {
				return
			}
			if queue.ShuffleOffset <= 0 {
				if -queue.ShuffleOffset <= len(queue.AheadUnshuffled) {
					queue.AheadUnshuffled = queue.AheadUnshuffled[-queue.ShuffleOffset:]
				}
				queue.ShuffleOffset = 0
			}
			queue.Ahead = append(queue.Ahead[:queue.ShuffleOffset], queue.AheadUnshuffled...)
			queue.AheadUnshuffled = nil
		}

		// Main IO loop
		for {
			select {
			case frame, ok := <-readAudioCh():
				if ok {
					outCh <- frame
					nFrames++
				} else {
					// Audio channel was closed -> stream is finished -> reset all stream channels
					audioch = nil
					errch = nil
					killch = nil

					if filePlaybackDoneCh != nil {
						filePlaybackDoneCh <- struct{}{}
					}
					filePlaybackDoneCh = nil

					fmt.Println("Audio channel closed, going to next track")
					if queue.Loop {
						refreshStream(0, playbackSpeed)
					} else {
						jumpTracks(1)
					}
				}
			case err, ok := <-errch:
				if ok {
					// Propagate error
					cErrCh <- err

					// Stream has closed with error -> reset all of its channels
					audioch = nil
					errch = nil
					killch = nil

					// Try to resurrect stream (if it fails again in the
					// next 5 seconds, we'll skip the track instead)
					now := time.Now()
					if lastStreamErr.Sub(now) > 5*time.Second {
						refreshStream(getPlaybackTime(), playbackSpeed)
					} else {
						jumpTracks(1)
						cErrCh <- errors.New("skipping stream due to multiple errors")
					}
					lastStreamErr = now
				} else {
					// Stream is done without err, but not fully read yet -> block
					// all future errch reads
					// Also, killch is now unnecessary
					errch = nil
					killch = nil
				}
			case cmd, ok := <-cCmdCh:
				if !ok {
					// cCmdCh was closed by the user -> client is told to shut down
					fmt.Println("Command channel closed, killing client")
					killStream()
					for _, c := range callbacksKilled {
						c(EventKilled{})
					}
					return
				} else {
					switch v := cmd.(type) {
					case CmdPlay:
						queue.Paused = false
						if audioch == nil {
							jumpTracks(1)
						}
					case CmdPause:
						queue.Paused = true
					case CmdLoop:
						queue.Loop = bool(v)
					case CmdJump:
						jumpTracks(int(v))
					case CmdSkipAll:
						killStream()
						if queue.Playing != nil {
							queue.Done = append(queue.Done, *queue.Playing)
							queue.Playing = nil
						}
						queue.Done = append(queue.Done, queue.Ahead...)
						queue.Ahead = nil
					case CmdShuffle:
						shuffle()
					case CmdUnshuffle:
						unshuffle()
					case CmdSwap:
						queue.AheadUnshuffled = nil
						sw := struct{ A, B int }(v)
						if queue.InBounds(sw.A) && queue.InBounds(sw.B) {
							replacePlaying := sw.A == 0 || sw.B == 0
							if replacePlaying {
								killStream()
							}
							*queue.At(sw.A), *queue.At(sw.B) = *queue.At(sw.B), *queue.At(sw.A)
							if replacePlaying {
								refreshStream(0, playbackSpeed)
							}
						}
					case CmdDelete:
						queue.AheadUnshuffled = nil
						idxs := []int(v)

						// Sort indices descendingly by absolute value so we don't
						// mess the future indices up in the process of removal
						sort.Slice(idxs, func(i, j int) bool {
							abs := func(i int) int {
								if i < 0 {
									return -i
								}
								return i
							}
							return idxs[abs(j)] < idxs[abs(i)]
						})

						for _, i := range idxs {
							if i < 0 {
								i = len(queue.Done) + i
								if i < len(queue.Done) {
									queue.Done = append(queue.Done[:i], queue.Done[i+1:]...)
								}
							} else if i == 0 {
								killStream()
								queue.Playing = nil
								refreshStream(0, playbackSpeed)
							} else {
								i -= 1
								if i < len(queue.Ahead) {
									queue.Ahead = append(queue.Ahead[:i], queue.Ahead[i+1:]...)
								}
							}
						}
					case CmdAddFront:
						queue.Ahead = append([]extractor.Data(v), queue.Ahead...)
						queue.ShuffleOffset++
					case CmdAddBack:
						queue.Ahead = append(queue.Ahead, []extractor.Data(v)...)
					case CmdSeek:
						if float64(v) > getPlaybackTime() && float64(v) < getMaxCachedPlaybackTime() {
							fmt.Println("Quick seeking to", v)
							// Seek to location in buffer
							for getPlaybackTime() < float64(v) {
								_, ok := <-audioch
								if !ok {
									break
								}
								nFrames++
							}
						} else {
							fmt.Println("Slow seeking to", v)
							// Restart stream from other location (seek using ffmpeg)
							refreshStream(float64(v), playbackSpeed)
						}
					case CmdSpeed:
						refreshStream(getPlaybackTime(), float64(v))
					case CmdPlayFileAndStop:
						cmd := struct {
							DoneCh chan<- struct{}
							Data   []byte
						}(v)

						audioch, errch, killch = audio.StreamToDiscordOpus(ffmpegPath, "pipe:", bytes.NewReader(cmd.Data), 0, 1.0, false)

						// Reset stream info
						nFrames = 0
						tStart = 0
						playbackSpeed = 1.0

						queue.Paused = false
						queue.Loop = false

						filePlaybackDoneCh = cmd.DoneCh
					case CmdGetTime:
						v <- getPlaybackTime()
					case CmdGetQueue:
						v <- queue.Copy()
					case CmdGetSpeed:
						v <- playbackSpeed
					}
				}
			}
		}
	}()

	return Client{CmdCh: cCmdCh, ErrCh: cErrCh}
}
