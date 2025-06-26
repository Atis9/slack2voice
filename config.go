package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	SlackBotToken      string
	SlackAppLevelToken string
	VoicevoxEndpoint   string
	VoicevoxSpeakerID  string
	UserIDs            []string `json:"user_ids"`
	ChannelIDs         []string `json:"channel_ids"`
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
		log.Println("INFO: USER_IDS not set; no messages will be read out based on user ID filter.")
	}

	channelIDsJSON := os.Getenv("CHANNEL_IDS")
	if channelIDsJSON != "" {
		if err := json.Unmarshal([]byte(channelIDsJSON), &cfg.ChannelIDs); err != nil {
			return nil, fmt.Errorf("failed to parse CHANNEL_IDS: %w", err)
		}
	} else {
		log.Println("INFO: CHANNEL_IDS not set; channel filtering will not be applied.")
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
