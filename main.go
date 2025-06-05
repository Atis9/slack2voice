package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ebitengine/oto/v3"
	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"time"
)

type Config struct {
	SlackBotToken      string
	SlackAppLevelToken string
	VoicevoxEndpoint   string
	VoicevoxSpeakerID  string
	UserIDs            []string `json:"user_ids"`
}

func loadConfig() (*Config, error) {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	cfg := &Config{}
	cfg.SlackBotToken = os.Getenv("SLACK_BOT_TOKEN")
	if cfg.SlackBotToken == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN must be set.")
	}
	cfg.SlackAppLevelToken = os.Getenv("SLACK_APP_LEVEL_TOKEN")
	if cfg.SlackAppLevelToken == "" {
		return nil, fmt.Errorf("SLACK_APP_LETEL_TOKEN must be set.")
	}
	userIDsJSON := os.Getenv("USER_IDS")
	if userIDsJSON != "" {
		if err := json.Unmarshal([]byte(userIDsJSON), &cfg.UserIDs); err != nil {
			return nil, fmt.Errorf("Failed to parse USER_IDS: %w", err)
		}
	} else {
		cfg.UserIDs = []string{}
	}
	cfg.VoicevoxEndpoint = os.Getenv("VOICEVOX_ENDPOINT")
	if cfg.VoicevoxEndpoint == "" {
		return nil, fmt.Errorf("VOICEVOX_ENDPOINT must be set.")
	}
	cfg.VoicevoxSpeakerID = os.Getenv("VOICEVOX_SPEAKER_ID")
	if cfg.VoicevoxSpeakerID == "" {
		return nil, fmt.Errorf("VOICEVOX_SPEAKER_ID must be set.")
	}

	return cfg, nil
}

func getAudioQuery(cfg *Config, text string) ([]byte, error) {
	req, err := http.NewRequest("POST", cfg.VoicevoxEndpoint+"/audio_query", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	speakerID := os.Getenv("VOICEVOX_SPEAKER_ID")
	q.Add("speaker", speakerID)
	q.Add("text", text)
	req.URL.RawQuery = q.Encode()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return bodyBytes, nil
}

func synthesis(cfg *Config, body []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", cfg.VoicevoxEndpoint+"/synthesis", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "audio/wav")
	req.Header.Add("Content-Type", "application/json")
	q := req.URL.Query()
	q.Add("speaker", cfg.VoicevoxSpeakerID)
	req.URL.RawQuery = q.Encode()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	buff := bytes.NewBuffer(nil)
	if _, err := io.Copy(buff, res.Body); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

func play(b []byte) error {
	op := &oto.NewContextOptions{}
	op.SampleRate = 24000
	op.ChannelCount = 1
	op.Format = oto.FormatSignedInt16LE
	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		log.Fatal(err)
	}
	<-readyChan
	player := otoCtx.NewPlayer(bytes.NewReader(b))
	player.Play()
	for player.IsPlaying() {
		time.Sleep(time.Millisecond)
	}
	err = player.Close()
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	api := slack.New(
		cfg.SlackBotToken,
		slack.OptionAppLevelToken(cfg.SlackAppLevelToken),
	)
	client := socketmode.New(
		api,
	)

	_, err = api.AuthTest()
	if err != nil {
		log.Fatalf("SLACK_BOT_TOKEN is invalid: %v\n", err)
	}

	for _, v := range cfg.UserIDs {
		user, err := api.GetUserInfo(v)
		if err != nil {
			log.Fatal(err)
			return
		}
		log.Printf("ID: %s, Name: %s", user.ID, user.Profile.DisplayName)
	}

	go func() {
		for envelope := range client.Events {
			switch envelope.Type {
			case socketmode.EventTypeEventsAPI:
				client.Ack(*envelope.Request)

				eventPayload, _ := envelope.Data.(slackevents.EventsAPIEvent)
				switch eventPayload.Type {
				case slackevents.CallbackEvent:
					switch event := eventPayload.InnerEvent.Data.(type) {
					case *slackevents.MessageEvent:
						if slices.Contains(cfg.UserIDs, event.User) {
							user, err := api.GetUserInfo(event.User)
							if err != nil {
								log.Fatal(err)
							}
							text := user.Profile.DisplayName + "さんからのメッセージ。" + event.Message.Text
							log.Print(text)
							body, err := getAudioQuery(cfg, text)
							if err != nil {
								log.Fatal(err)
							}
							b, err := synthesis(cfg, body)
							if err != nil {
								log.Fatal(err)
							}
							if err := play(b[44:]); err != nil {
								log.Fatal(err)
							}

						}
					}

				}
			}
		}
	}()

	client.Run()
}
