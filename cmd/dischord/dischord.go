package main

import (
	dc "github.com/bwmarrin/discordgo"

	"git.nobrain.org/r4/dischord/config"
	"git.nobrain.org/r4/dischord/extractor"
	_ "git.nobrain.org/r4/dischord/extractor/builtins"
	"git.nobrain.org/r4/dischord/extractor/ytdl"
	"git.nobrain.org/r4/dischord/player"
	"git.nobrain.org/r4/dischord/util"

	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	_ "embed"
)

var copyright bool
var autoconf bool
var registerCommands bool
var unregisterCommands bool

//go:embed bye.opus
var resByeOpus []byte

func init() {
	flag.BoolVar(&copyright, "copyright", false, "print copyright info and quit")
	flag.BoolVar(&autoconf, "autoconf", false, "launch automatic configurator program (overwriting any existing configuration)")
	flag.BoolVar(&registerCommands, "register_commands", true, "register commands with Discord upon startup")
	flag.BoolVar(&unregisterCommands, "unregister_commands", false, "unregister registered commands with Discord upon shutdown")
}

// A UserError is shown to the user
type UserError struct {
	error
}

const (
	interactionFlags = uint64(dc.MessageFlagsEphemeral)
)

var (
	ErrVoiceNotConnected               = UserError{errors.New("bot is currently not connected to a voice channel")}
	ErrUnsupportedUrl                  = UserError{errors.New("unsupported URL")}
	ErrStartThinkingNotInitialResponse = errors.New("StartThinking() must be the initial response")
	ErrInvalidAutocompleteCall         = errors.New("invalid autocomplete call")
)

type MessageData struct {
	Content    string
	Files      []*dc.File
	Components []dc.MessageComponent
	Embeds     []*dc.MessageEmbed
}

type MessageWriter struct {
	session     *dc.Session
	interaction *dc.Interaction
	first       bool
	thinking    bool
}

func NewMessageWriter(s *dc.Session, ia *dc.Interaction) *MessageWriter {
	return &MessageWriter{
		session:     s,
		interaction: ia,
		first:       true,
		thinking:    false,
	}
}

func (m *MessageWriter) StartThinking() error {
	if !m.first {
		return ErrStartThinkingNotInitialResponse
	}
	err := m.session.InteractionRespond(m.interaction, &dc.InteractionResponse{
		Type: dc.InteractionResponseDeferredChannelMessageWithSource,
		Data: &dc.InteractionResponseData{
			Flags: interactionFlags,
		},
	})
	if err != nil {
		return err
	}
	m.first = false
	m.thinking = true
	return nil
}

func (m *MessageWriter) Message(d *MessageData) error {
	var err error
	if m.first {
		err = m.session.InteractionRespond(m.interaction, &dc.InteractionResponse{
			Type: dc.InteractionResponseChannelMessageWithSource,
			Data: &dc.InteractionResponseData{
				Content:    d.Content,
				Flags:      interactionFlags,
				Files:      d.Files,
				Components: d.Components,
				Embeds:     d.Embeds,
			},
		})
	} else if m.thinking {
		_, err = m.session.InteractionResponseEdit(m.interaction, &dc.WebhookEdit{
			Content:    d.Content,
			Files:      d.Files,
			Components: d.Components,
			Embeds:     d.Embeds,
		})
	} else {
		_, err = m.session.FollowupMessageCreate(m.interaction, true, &dc.WebhookParams{
			Content:    d.Content,
			Flags:      interactionFlags,
			Files:      d.Files,
			Components: d.Components,
			Embeds:     d.Embeds,
		})
	}
	if err != nil {
		return err
	}
	m.first = false
	m.thinking = false
	return nil
}

func main() {
	flag.Parse()

	if copyright {
		fmt.Println(copyrightText)
		return
	}

	var clients sync.Map // guild ID string to player.Client

	// Load / create configuration file
	cfgfile := "config.toml"
	var cfg *config.Config
	var err error
	if autoconf || func() bool {cfg, err = config.Load(cfgfile); return err != nil}() {
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Configuration file not found, launching automatic configurator.")
				fmt.Println("Hit Ctrl+C to cancel anytime.")
			} else {
				fmt.Println("Error:", err)
				return
			}
		}
		cfg, err = config.Autoconf(cfgfile)
		if err != nil {
			if err == config.ErrPythonNotInstalled {
				if runtime.GOOS == "darwin" {
					fmt.Println("Python is required to run youtube-dl, but no python installation was found. To fix this, please install Xcode Command Line Tools.")
				} else {
					fmt.Println("Python is required to run youtube-dl, but no python installation was found. To fix this, please install Python from your package manager.")
				}
			} else {
				fmt.Println("Error:", err)
			}
			return
		}
		if runtime.GOOS == "windows" {
			fmt.Println("Hit Enter to close this window.")
			fmt.Scanln()
		}
		return
	}

	getClient := func(s *dc.Session, ia *dc.Interaction, create bool) (client player.Client, err error, created bool) {
		clI, exists := clients.Load(ia.GuildID)
		if exists {
			return clI.(player.Client), nil, false
		}

		if !create {
			return player.Client{}, ErrVoiceNotConnected, false
		}

		g, err := s.State.Guild(ia.GuildID)
		if err != nil {
			return player.Client{}, err, false
		}

		voiceChannelId := ""
		for _, v := range g.VoiceStates {
			if v.UserID == ia.Member.User.ID {
				voiceChannelId = v.ChannelID
				break
			}
		}

		if voiceChannelId == "" {
			return player.Client{}, UserError{errors.New("bot doesn't know where to join, please enter a voice channel")}, false
		}

		vc, err := s.ChannelVoiceJoin(ia.GuildID, voiceChannelId, false, true)
		if err != nil {
			return player.Client{}, err, false
		}

		cl := player.NewClient(cfg.Extractors, cfg.FfmpegPath, vc.OpusSend, func(e player.EventStreamUpdated) {
			if err := vc.Speaking(true); err != nil {
				fmt.Println("Unable to speak:", err)
			}
		}, func(e player.EventKilled) {
			vc.Disconnect()
		})

		clients.Store(ia.GuildID, cl)

		go func() {
			for err := range cl.ErrCh {
				fmt.Println("Playback error:", err)
			}
		}()

		return cl, nil, true
	}

	getOptions := func(d *dc.ApplicationCommandInteractionData) map[string]*dc.ApplicationCommandInteractionDataOption {
		opts := make(map[string]*dc.ApplicationCommandInteractionDataOption, len(d.Options))
		for _, v := range d.Options {
			opts[v.Name] = v
		}
		return opts
	}

	floatptr := func(f float64) *float64 {
		res := new(float64)
		*res = f
		return res
	}

	commands := []*dc.ApplicationCommand{
		{
			Name:        "queue",
			Description: "Show current playing queue",
		},
		{
			Name:        "play",
			Description: "Resume playback or replace playing queue",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "url-or-query",
					Description:  "Video/music/playlist URL or search query to start playing",
					Required:     false,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "add",
			Description: "Add a track or playlist to queue",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "url-or-query",
					Description:  "Video/music/playlist URL or search query to add to queue",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "pause",
			Description: "Pause playback",
		},
		{
			Name:        "loop",
			Description: "Toggle loop",
		},
		{
			Name:        "stop",
			Description: "Stop playback and disconnect",
		},
		{
			Name:        "disconnect",
			Description: "Alias for stop (stop playback and disconnect)",
		},
		{
			Name:        "dc",
			Description: "Alias for stop (stop playback and disconnect)",
		},
		{
			Name:        "jump",
			Description: "Jump to a track by number or name",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track",
					Description:  "Track number or matching string",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "seek",
			Description: "Jump to a playback position. Format is ss, mm:ss or hh:mm:ss. Use prefixes +/- to jump relatively",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:        dc.ApplicationCommandOptionString,
					Name:        "pos",
					Description: "Target playback position. Format is ss, mm:ss or hh:mm:ss. Use prefixes +/- to jump relatively",
					Required:    true,
				},
			},
		},
		{
			Name:        "pos",
			Description: "Get current playback position (time)",
		},
		{
			Name:        "speed",
			Description: "Get or set the playback speed",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:        dc.ApplicationCommandOptionNumber,
					Name:        "speed",
					Description: "New playback speed",
					Required:    false,
					MinValue:    floatptr(0.5),
					MaxValue:    3.0,
				},
			},
		},
		{
			Name:        "shuffle",
			Description: "Shuffle all items in the queue",
			Options:     []*dc.ApplicationCommandOption{},
		},
		{
			Name:        "unshuffle",
			Description: "Undoes what shuffle did (may no longer be available after certain queue modifications)",
			Options:     []*dc.ApplicationCommandOption{},
		},
		{
			Name:        "swap",
			Description: "Swap two items' positions in the queue",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-1",
					Description:  "Track number or matching string",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-2",
					Description:  "Track number or matching string",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "delete",
			Description: "Delete up to five items from the queue (use delete-from to delete even more at once)",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-1",
					Description:  "Track number or matching string",
					Required:     true,
					Autocomplete: true,
				},
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-2",
					Description:  "Track number or matching string",
					Required:     false,
					Autocomplete: true,
				},
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-3",
					Description:  "Track number or matching string",
					Required:     false,
					Autocomplete: true,
				},
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-4",
					Description:  "Track number or matching string",
					Required:     false,
					Autocomplete: true,
				},
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-5",
					Description:  "Track number or matching string",
					Required:     false,
					Autocomplete: true,
				},
			},
		},
		{
			Name:        "delete-from",
			Description: "Delete the specified track along with all tracks following it from the queue",
			Options: []*dc.ApplicationCommandOption{
				{
					Type:         dc.ApplicationCommandOptionString,
					Name:         "track-1",
					Description:  "Track number or matching string",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	}

	addToQueue := func(s *dc.Session, m *MessageWriter, cl player.Client, input string) error {
		if err := m.StartThinking(); err != nil {
			return err
		}

		data, err := extractor.Extract(cfg.Extractors, input)
		if err != nil {
			if exerr, ok := err.(*extractor.Error); ok && exerr.Err == ytdl.ErrUnsupportedUrl {
				return ErrUnsupportedUrl
			}
			return err
		}

		cl.CmdCh <- player.CmdAddBack(data)

		var msg string
		if len(data) == 1 {
			msg = fmt.Sprintf("Added %v to queue", data[0].Title)
		} else if len(data) > 0 {
			msg = fmt.Sprintf("Added playlist %v to queue (%v items)", data[0].PlaylistTitle, len(data))
		} else {
			return UserError{errors.New("extractor returned no results")}
		}

		if err := m.Message(&MessageData{Content: msg}); err != nil {
			return err
		}
		return nil
	}

	matchTracks := func(cl player.Client, search string, n int) []struct {
		title  string
		relIdx int
	} {
		var res []struct {
			title  string
			relIdx int
		}
		maybeAdd := func(title string, relIdx int) {
			if strings.Contains(strings.ToLower(title), strings.ToLower(search)) {
				res = append(res, struct {
					title  string
					relIdx int
				}{
					title:  title,
					relIdx: relIdx,
				})
			}
		}
		queue := cl.GetQueue()
		for i, v := range queue.Done {
			maybeAdd(v.Title, i-len(queue.Done))
		}
		if queue.Playing != nil {
			maybeAdd(queue.Playing.Title, 0)
		}
		for i, v := range queue.Ahead {
			maybeAdd(v.Title, i+1)
		}
		sort.Slice(res, func(i, j int) bool {
			cost := func(s string) int {
				return strings.Index(strings.ToLower(s), strings.ToLower(search))
			}
			return cost(res[i].title) < cost(res[j].title)
		})
		if n < len(res) {
			return res[:n]
		} else {
			return res
		}
	}

	checkQueueBounds := func(queue *player.Queue, i int) error {
		if i == 0 && queue.Playing == nil {
			return UserError{errors.New("track index 0 is invalid when no track is playing")}
		}
		if i < 0 && -i-1 >= len(queue.Done) {
			return UserError{fmt.Errorf("track index %v is too low (minimum is %v)", i, -len(queue.Done))}
		}
		if i > 0 && i-1 >= len(queue.Ahead) {
			return UserError{fmt.Errorf("track index %v is too high (maximum is %v)", i, len(queue.Ahead))}
		}
		return nil
	}

	getTrackNum := func(cl player.Client, input string) (int, error) {
		n, err := strconv.Atoi(input)
		if err != nil {
			tracks := matchTracks(cl, input, 1)
			if len(tracks) > 0 {
				return tracks[0].relIdx, nil
			} else {
				return 0, UserError{errors.New("no matching track found")}
			}
		}
		return n, nil
	}

	getTrackEmbed := func(queue *player.Queue, i int) *dc.MessageEmbed {
		if !queue.InBounds(i) {
			return nil
		}

		track := queue.At(i)
		url := track.SourceUrl
		id := strings.TrimSuffix(strings.TrimPrefix(url, "https://www.youtube.com/watch?v="), "/")
		var desc string
		if i == 0 {
			if queue.Paused {
				desc = "Paused"
			} else {
				desc = "Playing"
			}
			if queue.Loop {
				desc += " (loop)"
			}
		} else {
			desc = strconv.Itoa(i)
		}
		return &dc.MessageEmbed{
			Title:       track.Title,
			Description: desc,
			URL:         url + "/" + strconv.Itoa(i),
			Thumbnail: &dc.MessageEmbedThumbnail{
				URL: "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg",
			},
		}
	}

	var commandHandlers map[string]func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error
	commandHandlers = map[string]func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error{
		"queue": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}

			queue := cl.GetQueue()

			if len(queue.Done) == 0 && queue.Playing == nil && len(queue.Ahead) == 0 {
				if err := m.Message(&MessageData{Content: "Queue is empty"}); err != nil {
					return err
				}
				return nil
			}

			var embeds []*dc.MessageEmbed
			trySend := func(flush bool) error {
				if len(embeds) >= 10 || (len(embeds) > 0 && flush) {
					err := m.Message(&MessageData{
						Embeds: embeds,
					})
					if err != nil {
						return err
					}
					embeds = nil
				}
				return nil
			}

			for i := -len(queue.Done); i <= len(queue.Ahead); i++ {
				if i == 0 && queue.Playing == nil {
					continue
				}
				embeds = append(embeds, getTrackEmbed(queue, i))
				if err := trySend(false); err != nil {
					return err
				}
			}
			if err := trySend(true); err != nil {
				return err
			}
			return nil
		},
		"play": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			opts := getOptions(d)
			inputI, exists := opts["url-or-query"]
			if exists {
				cl, err, _ := getClient(s, ia, true)
				if err != nil {
					return err
				}

				input := inputI.StringValue()

				cl.CmdCh <- player.CmdSkipAll{}

				err = addToQueue(s, m, cl, input)
				if err != nil {
					return err
				}

				cl.CmdCh <- player.CmdPlay{}
			} else {
				cl, err, _ := getClient(s, ia, false)
				if err != nil {
					return err
				}

				queue := cl.GetQueue()

				if queue.Paused {
					cl.CmdCh <- player.CmdPlay{}
					if err := m.Message(&MessageData{Content: "Playback resumed"}); err != nil {
						return err
					}
				} else if queue.Playing == nil && len(queue.Ahead) > 0 {
					cl.CmdCh <- player.CmdPlay{}
					if err := m.Message(&MessageData{Content: "Started playing"}); err != nil {
						return err
					}
				} else if queue.Playing == nil && len(queue.Ahead) == 0 {
					return UserError{errors.New("nothing in queue to resume from")}
				} else {
					return UserError{errors.New("already playing")}
				}
			}
			return nil
		},
		"add": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, created := getClient(s, ia, true)
			if err != nil {
				return err
			}

			err = addToQueue(s, m, cl, d.Options[0].StringValue())
			if err != nil {
				return err
			}

			if created {
				if err := m.Message(&MessageData{Content: "Use /play to start playing"}); err != nil {
					return err
				}
			}
			return nil
		},
		"pause": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			if cl.GetQueue().Paused {
				return UserError{errors.New("already paused")}
			} else {
				cl.CmdCh <- player.CmdPause{}
				if err := m.Message(&MessageData{Content: "Playback paused"}); err != nil {
					return err
				}
			}
			return nil
		},
		"loop": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			loop := cl.GetQueue().Loop
			cl.CmdCh <- player.CmdLoop(!loop)
			var msg string
			if loop {
				msg = "Loop disabled"
			} else {
				msg = "Loop enabled"
			}
			if err := m.Message(&MessageData{Content: msg}); err != nil {
				return err
			}
			return nil
		},
		"stop": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			ch := make(chan struct{})
			cl.CmdCh <- player.CmdPlayFileAndStop{ch, resByeOpus}
			if err := m.Message(&MessageData{Content: "Bye, have a great time"}); err != nil {
				return err
			}
			<-ch
			clients.Delete(ia.GuildID)
			close(cl.CmdCh)
			return nil
		},
		"disconnect": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			return commandHandlers["stop"](s, m, ia, d)
		},
		"dc": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			return commandHandlers["stop"](s, m, ia, d)
		},
		"jump": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			track := d.Options[0].StringValue()
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}

			n, err := getTrackNum(cl, track)
			if err != nil {
				return err
			}

			cl.CmdCh <- player.CmdJump(n)

			var msg string
			queue := cl.GetQueue()
			if queue.Playing != nil {
				msg = fmt.Sprintf("Jumped to %v", queue.Playing.Title)
			} else {
				msg = "Playback finished"
			}
			if err := m.Message(&MessageData{Content: msg}); err != nil {
				return err
			}
			return nil
		},
		"seek": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}

			var relFactor int
			input := d.Options[0].StringValue()
			if strings.HasPrefix(input, "+") {
				relFactor = 1
				input = strings.TrimPrefix(input, "+")
			} else if strings.HasPrefix(input, "-") {
				relFactor = -1
				input = strings.TrimPrefix(input, "-")
			}

			ntI, err := util.ParseDurationSeconds(input)
			if err != nil {
				return UserError{errors.New("invalid time format")}
			}

			queue := cl.GetQueue()

			if queue.Playing != nil {
				time := int(cl.GetTime())
				d := util.FormatDurationSeconds(int(queue.Playing.Duration))
				if relFactor != 0 {
					ntI = time + relFactor*ntI
				}
				if ntI < 0 || ntI >= queue.Playing.Duration {
					return UserError{errors.New("time out of range")}
				}
				nt := util.FormatDurationSeconds(ntI)
				relI := ntI - time
				var rel string
				if relI < 0 {
					rel = "-" + util.FormatDurationSeconds(-relI)
				} else {
					rel = "+" + util.FormatDurationSeconds(relI)
				}
				cl.CmdCh <- player.CmdSeek(int64(ntI))
				if err := m.Message(&MessageData{Content: fmt.Sprintf("Sought to: %v/%v (%v)", nt, d, rel)}); err != nil {
					return err
				}
			} else {
				return UserError{errors.New("not playing anything")}
			}
			return nil
		},
		"pos": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}

			queue := cl.GetQueue()

			if queue.Playing != nil {
				time := cl.GetTime()
				t := util.FormatDurationSeconds(int(time))
				d := util.FormatDurationSeconds(int(queue.Playing.Duration))

				err := m.Message(&MessageData{
					Content: fmt.Sprintf("Position: %v/%v", t, d),
					Embeds: []*dc.MessageEmbed{
						getTrackEmbed(queue, 0),
					},
				})
				if err != nil {
					return err
				}
			} else {
				return UserError{errors.New("not playing anything")}
			}
			return nil
		},
		"speed": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			opts := getOptions(d)
			inputI, exists := opts["speed"]

			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}

			if exists {
				speed := inputI.FloatValue()
				cl.CmdCh <- player.CmdSpeed(speed)
				if err := m.Message(&MessageData{Content: fmt.Sprintf("Playing at %vx speed", speed)}); err != nil {
					return err
				}
				return nil
			} else {
				if err := m.Message(&MessageData{Content: fmt.Sprintf("Current playback speed: %vx", cl.GetSpeed())}); err != nil {
					return err
				}
				return nil
			}
		},
		"shuffle": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			cl.CmdCh <- player.CmdShuffle{}
			if err := m.Message(&MessageData{Content: fmt.Sprintf("Shuffled queue (%v items)", len(cl.GetQueue().Ahead))}); err != nil {
				return err
			}
			return nil
		},
		"unshuffle": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			queue := cl.GetQueue()
			if queue.AheadUnshuffled == nil {
				return UserError{errors.New("cannot unshuffle queue: either it is not shuffled, or too many modifications have been made to reverse the shuffle")}
			}
			cl.CmdCh <- player.CmdUnshuffle{}
			if err := m.Message(&MessageData{Content: fmt.Sprintf("Unshuffled queue (%v items)", len(queue.Ahead))}); err != nil {
				return err
			}
			return nil
		},
		"swap": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			queue := cl.GetQueue()
			sa, sb := d.Options[0].StringValue(), d.Options[1].StringValue()

			var a, b int
			a, err = getTrackNum(cl, sa)
			if err != nil {
				return err
			}
			b, err = getTrackNum(cl, sb)
			if err != nil {
				return err
			}

			if err := checkQueueBounds(queue, a); err != nil {
				return err
			}
			if err := checkQueueBounds(queue, b); err != nil {
				return err
			}

			ta, tb := queue.At(a), queue.At(b)
			cl.CmdCh <- player.CmdSwap{a, b}
			if err := m.Message(&MessageData{Content: fmt.Sprintf("Swapped item %v: '%v' with %v: '%v'", ta.Title, tb.Title)}); err != nil {
				return err
			}
			return nil
		},
		"delete": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			queue := cl.GetQueue()
			var toDel []int
			for _, opt := range d.Options {
				a, err := getTrackNum(cl, opt.StringValue())
				if err != nil {
					return err
				}
				if err := checkQueueBounds(queue, a); err != nil {
					return err
				}
				toDel = append(toDel, a)
			}
			cl.CmdCh <- player.CmdDelete(toDel)
			var msg string
			msg = "Deleted "
			for _, i := range toDel {
				msg += fmt.Sprintf("%v: '%v'", i, queue.At(i).Title)
				if i == len(toDel)-2 {
					msg += "and "
				} else if i != len(toDel)-1 {
					msg += ", "
				}
			}
			msg += " from queue"
			if err := m.Message(&MessageData{Content: msg}); err != nil {
				return err
			}
			return nil
		},
		"delete-from": func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			cl, err, _ := getClient(s, ia, false)
			if err != nil {
				return err
			}
			queue := cl.GetQueue()
			a, err := getTrackNum(cl, d.Options[0].StringValue())
			if err != nil {
				return err
			}
			if err := checkQueueBounds(queue, a); err != nil {
				return err
			}
			var toDel []int
			i := a
			for {
				if i != 0 && queue.At(i) == nil {
					break
				}
				toDel = append(toDel, i)
				i++
			}
			cl.CmdCh <- player.CmdDelete(toDel)
			if err := m.Message(&MessageData{Content: fmt.Sprintf("Deleted %v items starting with %v: '%v'", len(toDel), a, queue.At(a).Title)}); err != nil {
				return err
			}
			return nil
		},
	}

	autocompleteBySearch := func(s *dc.Session, ia *dc.Interaction, input string) error {
		var choices []*dc.ApplicationCommandOptionChoice
		if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
			choices = []*dc.ApplicationCommandOptionChoice{
				{
					Name:  input,
					Value: input,
				},
			}
		} else if input != "" {
			res, err := extractor.Search(cfg.Extractors, input)
			if err != nil {
				return err
			}

			choices = make([]*dc.ApplicationCommandOptionChoice, len(res))
			for i, v := range res {
				switch {
				case v.Title != "":
					var prefix string
					if v.OfficialArtist {
						prefix = "🎵 "
					}
					choices[i] = &dc.ApplicationCommandOptionChoice{
						Name:  prefix + v.Title,
						Value: v.SourceUrl,
					}
				case v.PlaylistTitle != "":
					choices[i] = &dc.ApplicationCommandOptionChoice{
						Name:  "𝘗𝘭𝘢𝘺𝘭𝘪𝘴𝘵: " + v.PlaylistTitle,
						Value: v.PlaylistUrl,
					}
				}
			}
		}

		err = s.InteractionRespond(ia, &dc.InteractionResponse{
			Type: dc.InteractionApplicationCommandAutocompleteResult,
			Data: &dc.InteractionResponseData{
				Choices: choices,
			},
		})
		if err != nil {
			return err
		}
		return nil
	}

	autocompleteTrack := func(s *dc.Session, ia *dc.Interaction, input string) error {
		cl, err, _ := getClient(s, ia, false)
		if err != nil {
			if errors.Is(err, ErrVoiceNotConnected) {
				err = s.InteractionRespond(ia, &dc.InteractionResponse{
					Type: dc.InteractionApplicationCommandAutocompleteResult,
					Data: &dc.InteractionResponseData{},
				})
				if err != nil {
					return err
				}
				return nil
			}
			return err
		}

		tracks := matchTracks(cl, input, 10)

		choices := make([]*dc.ApplicationCommandOptionChoice, len(tracks))
		for i := range tracks {
			choices[i] = &dc.ApplicationCommandOptionChoice{
				Name:  tracks[i].title,
				Value: strconv.Itoa(tracks[i].relIdx),
			}
		}

		err = s.InteractionRespond(ia, &dc.InteractionResponse{
			Type: dc.InteractionApplicationCommandAutocompleteResult,
			Data: &dc.InteractionResponseData{
				Choices: choices,
			},
		})
		if err != nil {
			return err
		}
		return nil
	}

	autocompleteHandlers := map[string]func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error{
		"play": func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			opts := getOptions(d)
			inputI, exists := opts["url-or-query"]
			if !exists {
				return nil
			}
			input := inputI.StringValue()

			return autocompleteBySearch(s, ia, input)
		},
		"add": func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			return autocompleteBySearch(s, ia, d.Options[0].StringValue())
		},
		"jump": func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			return autocompleteTrack(s, ia, d.Options[0].StringValue())
		},
		"swap": func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			for i := 0; i < 5; i++ {
				if d.Options[i].Focused {
					return autocompleteTrack(s, ia, d.Options[i].StringValue())
				}
			}
			return ErrInvalidAutocompleteCall
		},
		"delete": func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			for i := 0; i < 5; i++ {
				if d.Options[i].Focused {
					return autocompleteTrack(s, ia, d.Options[i].StringValue())
				}
			}
			return ErrInvalidAutocompleteCall
		},
		"delete-from": func(s *dc.Session, ia *dc.Interaction, d *dc.ApplicationCommandInteractionData) error {
			return autocompleteTrack(s, ia, d.Options[0].StringValue())
		},
	}

	componentHandlers := map[string]func(s *dc.Session, m *MessageWriter, ia *dc.Interaction, d *dc.MessageComponentInteractionData) error{}

	// Create Discord session
	dg, err := dc.New("Bot " + cfg.Token)
	if err != nil {
		fmt.Println("Error creating Discord session:", err)
		return
	}
	dg.Identify.Intents = dc.IntentsAllWithoutPrivileged

	// Set up handlers
	readyCh := make(chan string)
	dg.AddHandler(func(s *dc.Session, e *dc.Ready) {
		u := s.State.User
		readyCh <- u.Username + "#" + u.Discriminator
	})
	dg.AddHandler(func(s *dc.Session, e *dc.InteractionCreate) {
		switch e.Type {
		case dc.InteractionApplicationCommand:
			d := e.ApplicationCommandData()
			m := NewMessageWriter(s, e.Interaction)
			if e.GuildID == "" {
				if err := m.Message(&MessageData{Content: "This bot only works on servers"}); err != nil {
					fmt.Printf("Error: %v\n", err)
				}
				return
			}
			if h, exists := commandHandlers[d.Name]; exists {
				if err := h(s, m, e.Interaction, &d); err != nil {
					if _, ok := err.(UserError); ok {
						if err := m.Message(&MessageData{Content: util.CapitalizeFirst(err.Error())}); err != nil {
							fmt.Printf("Error: %v\n", err)
						}
					} else {
						fmt.Printf("Error in commandHandlers[%v]: %v\n", d.Name, err)
						if err := m.Message(&MessageData{Content: "An internal error occurred :("}); err != nil {
							fmt.Printf("Error: %v\n", err)
						}
					}
				}
			}
		case dc.InteractionApplicationCommandAutocomplete:
			d := e.ApplicationCommandData()
			if h, exists := autocompleteHandlers[d.Name]; exists {
				if err := h(s, e.Interaction, &d); err != nil {
					fmt.Printf("Error in autocompleteHandlers[%v]: %v\n", d.Name, err)
				}
			}
		case dc.InteractionMessageComponent:
			d := e.MessageComponentData()
			m := NewMessageWriter(s, e.Interaction)
			if h, exists := componentHandlers[d.CustomID]; exists {
				if err := h(s, m, e.Interaction, &d); err != nil {
					if _, ok := err.(UserError); ok {
						if err := m.Message(&MessageData{Content: util.CapitalizeFirst(err.Error())}); err != nil {
							fmt.Printf("Error: %v\n", err)
						}
					} else {
						fmt.Printf("Error in componentHandlers[%v]: %v\n", d.CustomID, err)
					}
				}
			}
		default:
			fmt.Println("Unhandled interaction type:", e.Type)
		}
	})

	// Open Discord session
	err = dg.Open()
	if err != nil {
		fmt.Println("Error opening Discord session:", err)
		return
	}

	// Wait until discord session ready
	fmt.Printf("Logged in as %v\n", <-readyCh)

	// Set up commands
	if registerCommands {
		fmt.Println("Registering commands...")
		for i, v := range commands {
			cmd, err := dg.ApplicationCommandCreate(dg.State.User.ID, "", v)
			if err != nil {
				fmt.Printf("Error adding command %v: %v\n", v.Name, err)
				return
			}
			commands[i] = cmd
			fmt.Printf(" %v/%v done\r", i+1, len(commands))
		}
		fmt.Printf("%v commands registered\n", len(commands))
	}

	// Exit gracefully when the program is terminated
	fmt.Println("Bot is now running, press Ctrl+C to stop")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
	fmt.Println()
	fmt.Println("Received stop signal, shutting down cleanly")
	clients.Range(func(key, value any) bool {
		cl := value.(player.Client)
		close(cl.CmdCh)
		return true
	})
	if registerCommands && unregisterCommands {
		fmt.Println("Unregistering commands...")
		for i, v := range commands {
			err := dg.ApplicationCommandDelete(dg.State.User.ID, "", v.ID)
			if err != nil {
				fmt.Printf("Error deleting command %v: %v\n", v.Name, err)
				return
			}
			fmt.Printf(" %v/%v done\r", i+1, len(commands))
		}
		fmt.Printf("%v commands unregistered\n", len(commands))
	}
	dg.Close()
}
