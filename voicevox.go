package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	httpClientTimeout = 15 * time.Second
)

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
