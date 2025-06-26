package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	voicevoxAPITimeout = 20 * time.Second
	wavHeaderSize      = 44
	reactionSpeaker    = "speaker"
)

var slackMsgRegex = regexp.MustCompile(`<([^|>]+?)(\|(.+?))?>`)

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
			}
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
	}
}

func handleMessageEvent(slackAPI *slack.Client, cfg *Config, vvClient *VoicevoxClient, event *slackevents.MessageEvent) {
	if len(cfg.UserIDs) > 0 {
		if !slices.Contains(cfg.UserIDs, event.User) {
			return
		}
	}

	if len(cfg.ChannelIDs) > 0 {
		if !slices.Contains(cfg.ChannelIDs, event.Channel) {
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), voicevoxAPITimeout)
	defer cancel()

	processedText := slackMsgRegex.ReplaceAllStringFunc(event.Text, func(match string) string {
		submatches := slackMsgRegex.FindStringSubmatch(match)
		if len(submatches) > 3 && submatches[3] != "" {
			return submatches[3]
		}
		return ""
	})

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

	textToSpeak := fmt.Sprintf("%sさんからのメッセージ。%s", displayName, processedText)
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

	pcmDataSize := len(wavData) - wavHeaderSize
	log.Printf("INFO: Playing audio for \"%s\" (WAV size: %d bytes, PCM size: %d bytes)", textToSpeak, len(wavData), pcmDataSize)

	audioMutex.Lock()
	defer audioMutex.Unlock()

	itemRef := slack.NewRefToMessage(event.Channel, event.TimeStamp)

	errAddReaction := slackAPI.AddReactionContext(ctx, reactionSpeaker, itemRef)
	if errAddReaction != nil {
		log.Printf("WARNING: Failed to add reaction ':%s:' to message TS %s in channel %s: %v", reactionSpeaker, event.TimeStamp, event.Channel, errAddReaction)
	} else {
		log.Printf("INFO: Added reaction ':%s:' to message TS %s in channel %s", reactionSpeaker, event.TimeStamp, event.Channel)
	}

	if err := playAudio(wavData[wavHeaderSize:]); err != nil {
		log.Printf("ERROR: Failed to play audio for \"%s\": %v", textToSpeak, err)
	} else {
		log.Printf("INFO: Finished playing audio for \"%s\"", textToSpeak)
	}

	errRemoveReaction := slackAPI.RemoveReactionContext(ctx, reactionSpeaker, itemRef)
	if errRemoveReaction != nil {
		log.Printf("WARNING: Failed to remove reaction ':%s:' to message TS %s in channel %s: %v", reactionSpeaker, event.TimeStamp, event.Channel, errRemoveReaction)
	} else {
		log.Printf("INFO: Removed reaction ':%s:' to message TS %s in channel %s", reactionSpeaker, event.TimeStamp, event.Channel)
	}

}
