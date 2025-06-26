package main

import (
	"log"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("FATAL: Error loading configuration: %v", err)
	}

	if err := initAudio(); err != nil {
		log.Fatalf("FATAL: %v", err)
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
		log.Println("INFO: No specific UserIDs configured. User filtering will not be applied.")
	}

	if len(cfg.ChannelIDs) > 0 {
		log.Println("INFO: Messages will be filtered to the following Channel IDs:")
		for _, channelID := range cfg.ChannelIDs {
			log.Printf("  - Target Channel: ID=%s", channelID)
		}
	} else {
		log.Println("INFO: No specific ChannelIDs configured. Channel filtering will not be applied.")
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