package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	httpClientTimeout     = 15 * time.Second
	voicevoxAPITimeout    = 20 * time.Second
	wavHeaderSize         = 44
	audioPlayPollInterval = 50 * time.Millisecond
)

type Config struct {
	SlackBotToken      string
	SlackAppLevelToken string
	VoicevoxEndpoint   string
	VoicevoxSpeakerID  string
	UserIDs            []string `json:"user_ids"`
}

func loadConfig() (*Config, error) {
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("INFO: Could not load .env file: %v. Will rely on environment variables.", err)
	}

	cfg := &Config{}
	var missingEnvVars []string

	cfg.SlackBotToken = os.Getenv("SLACK_BOT_TOKEN")
	if cfg.SlackBotToken == "" {
		missingEnvVars = append(missingEnvVars, "SLACK_BOT_TOKEN")
	}

	cfg.SlackAppLevelToken = os.Getenv("SLACK_APP_LEVEL_TOKEN")
	if cfg.SlackAppLevelToken == "" {

		missingEnvVars = append(missingEnvVars, "SLACK_APP_LEVEL_TOKEN")
	}

	userIDsJSON := os.Getenv("USER_IDS")
	if userIDsJSON != "" {
		if err := json.Unmarshal([]byte(userIDsJSON), &cfg.UserIDs); err != nil {
			return nil, fmt.Errorf("failed to parse USER_IDS: %w", err)
		}
	} else {
		cfg.UserIDs = []string{}
		log.Println("INFO: USER_IDS not set; no specific users will be targeted for readout unless specified.")
	}

	cfg.VoicevoxEndpoint = os.Getenv("VOICEVOX_ENDPOINT")
	if cfg.VoicevoxEndpoint == "" {
		missingEnvVars = append(missingEnvVars, "VOICEVOX_ENDPOINT")
	}
	cfg.VoicevoxSpeakerID = os.Getenv("VOICEVOX_SPEAKER_ID")
	if cfg.VoicevoxSpeakerID == "" {
		missingEnvVars = append(missingEnvVars, "VOICEVOX_SPEAKER_ID")
	}

	if len(missingEnvVars) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missingEnvVars, ", "))
	}

	return cfg, nil
}

type VoicevoxClient struct {
	endpoint   string
	speakerID  string
	httpClient *http.Client
}

func NewVoicevoxClient(endpoint, speakerID string) *VoicevoxClient {
	return &VoicevoxClient{
		endpoint:  endpoint,
		speakerID: speakerID,
		httpClient: &http.Client{
			Timeout: httpClientTimeout,
		},
	}
}

func (vc *VoicevoxClient) GetAudioQuery(ctx context.Context, text string) ([]byte, error) {

	queryURL, err := url.JoinPath(vc.endpoint, "audio_query")
	if err != nil {
		return nil, fmt.Errorf("failed to create audio_query URL path: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create audio_query request: %w", err)
	}

	q := req.URL.Query()
	q.Add("speaker", vc.speakerID)
	q.Add("text", text)
	req.URL.RawQuery = q.Encode()

	res, err := vc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audio_query request execution failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("VOICEVOX API error (audio_query): status %s, body: %s", res.Status, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio_query response body: %w", err)
	}
	return bodyBytes, nil
}

func (vc *VoicevoxClient) Synthesis(ctx context.Context, audioQueryJSON []byte) ([]byte, error) {

	synthesisURL, err := url.JoinPath(vc.endpoint, "synthesis")
	if err != nil {
		return nil, fmt.Errorf("failed to create synthesis URL path: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", synthesisURL, bytes.NewReader(audioQueryJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create synthesis request: %w", err)
	}

	req.Header.Set("Accept", "audio/wav")
	req.Header.Set("Content-Type", "application/json")

	q := req.URL.Query()
	q.Add("speaker", vc.speakerID)
	req.URL.RawQuery = q.Encode()

	res, err := vc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("synthesis request execution failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("VOICEVOX API error (synthesis): status %s, body: %s", res.Status, string(bodyBytes))
	}

	wavData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read synthesis response body: %w", err)
	}
	return wavData, nil
}

func playAudio(pcmData []byte) error {
	op := &oto.NewContextOptions{}
	op.SampleRate = 24000
	op.ChannelCount = 1
	op.Format = oto.FormatSignedInt16LE

	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return fmt.Errorf("failed to create oto context: %w", err)
	}
	<-readyChan

	player := otoCtx.NewPlayer(bytes.NewReader(pcmData))
	defer player.Close()

	player.Play()
	for player.IsPlaying() {
		time.Sleep(audioPlayPollInterval)
	}

	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("FATAL: Error loading configuration: %v", err)
	}

	slackAPI := slack.New(
		cfg.SlackBotToken,
		slack.OptionAppLevelToken(cfg.SlackAppLevelToken),

	)

	if _, err := slackAPI.AuthTest(); err != nil {
		log.Fatalf("FATAL: Slack API AuthTest failed (check tokens): %v", err)
	}
	log.Println("INFO: Slack API authentication successful.")

	if len(cfg.UserIDs) > 0 {
		log.Println("INFO: Will attempt to read out messages from the following User IDs:")
		for _, userID := range cfg.UserIDs {

			userInfo, err := slackAPI.GetUserInfo(userID)
			if err != nil {
				log.Printf("WARNING: Could not fetch info for target User ID %s: %v. Bot will still try to match this ID.", userID, err)
				continue
			}
			log.Printf("  - Target User: ID=%s, Name=%s", userInfo.ID, userInfo.Profile.DisplayName)
		}
	} else {
		log.Println("INFO: No specific UserIDs configured. Bot will not read out messages based on user ID filter.")
	}

	vvClient := NewVoicevoxClient(cfg.VoicevoxEndpoint, cfg.VoicevoxSpeakerID)
	log.Printf("INFO: VoicevoxClient initialized for endpoint %s with speaker ID %s", cfg.VoicevoxEndpoint, cfg.VoicevoxSpeakerID)

	socketClient := socketmode.New(slackAPI)

	log.Println("INFO: Starting Slack event listener...")
	go runEventLoop(socketClient, slackAPI, cfg, vvClient)

	if err := socketClient.Run(); err != nil {
		log.Fatalf("FATAL: Socketmode client exited with error: %v", err)
	}
}

func runEventLoop(client *socketmode.Client, slackAPI *slack.Client, cfg *Config, vvClient *VoicevoxClient) {
	for envelope := range client.Events {
		switch envelope.Type {
		case socketmode.EventTypeConnecting:
			log.Println("INFO: Slack Socketmode: Connecting...")
		case socketmode.EventTypeConnected:
			log.Println("INFO: Slack Socketmode: Connected.")
		case socketmode.EventTypeConnectionError:
			log.Printf("ERROR: Slack Socketmode: Connection error: %v", envelope.Data)
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := envelope.Data.(slackevents.EventsAPIEvent)
			if !ok {
				log.Printf("WARNING: Received unexpected data type for EventsAPI: %T", envelope.Data)
				client.Ack(*envelope.Request)
				continue
			}

			client.Ack(*envelope.Request)

			switch eventsAPIEvent.Type {
			case slackevents.CallbackEvent:

				go processCallbackEvent(slackAPI, cfg, vvClient, eventsAPIEvent.InnerEvent)
			default:

			}
		default:

		}
	}
}

func processCallbackEvent(slackAPI *slack.Client, cfg *Config, vvClient *VoicevoxClient, innerEvent slackevents.EventsAPIInnerEvent) {
	switch event := innerEvent.Data.(type) {
	case *slackevents.MessageEvent:

		if event.User == "" || event.BotID != "" || event.SubType == "bot_message" || event.SubType == "slackbot_response" {
			return
		}

		handleMessageEvent(slackAPI, cfg, vvClient, event)
	default:

	}
}

func handleMessageEvent(slackAPI *slack.Client, cfg *Config, vvClient *VoicevoxClient, event *slackevents.MessageEvent) {
	if !slices.Contains(cfg.UserIDs, event.User) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), voicevoxAPITimeout)
	defer cancel()

	var displayName string
	userInfo, err := slackAPI.GetUserInfoContext(ctx, event.User)
	if err != nil {
		log.Printf("WARNING: Failed to get user info for UserID %s: %v. Using UserID as fallback name.", event.User, err)
		displayName = event.User
	} else {
		displayName = userInfo.Profile.DisplayName
		if displayName == "" {
			displayName = userInfo.RealName
		}
		if displayName == "" {
			displayName = userInfo.Name
		}
		if displayName == "" {
			displayName = "ユーザー"
		}
	}

	textToSpeak := fmt.Sprintf("%sさんからのメッセージ。 %s", displayName, event.Text)
	log.Printf("INFO: Preparing to speak: \"%s\"", textToSpeak)

	audioQueryJSON, err := vvClient.GetAudioQuery(ctx, textToSpeak)
	if err != nil {
		log.Printf("ERROR: Failed to get audio query for \"%s\": %v", textToSpeak, err)
		return
	}

	wavData, err := vvClient.Synthesis(ctx, audioQueryJSON)
	if err != nil {
		log.Printf("ERROR: Failed to synthesize audio for \"%s\": %v", textToSpeak, err)
		return
	}

	if len(wavData) <= wavHeaderSize {
		log.Printf("ERROR: Synthesized WAV data is too short (length %d) for \"%s\"", len(wavData), textToSpeak)
		return
	}

	log.Printf("INFO: Playing audio for \"%s\" (PCM data size: %d bytes)", textToSpeak, len(wavData)-wavHeaderSize)
	if err := playAudio(wavData[wavHeaderSize:]); err != nil {
		log.Printf("ERROR: Failed to play audio for \"%s\": %v", textToSpeak, err)
	} else {
		log.Printf("INFO: Finished playing audio for \"%s\"", textToSpeak)
	}
}
