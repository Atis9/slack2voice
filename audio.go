package main

import (
	"bytes"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

const (
	audioPlayPollInterval = 50 * time.Millisecond
	otoSampleRate         = 24000
	otoChannelCount       = 1
	otoContextTimeout     = 5 * time.Second
)

var (
	audioMutex   sync.Mutex
	globalOtoCtx *oto.Context
)

func initAudio() error {
	op := &oto.NewContextOptions{}
	op.SampleRate = otoSampleRate
	op.ChannelCount = otoChannelCount
	op.Format = oto.FormatSignedInt16LE

	var readyChan <-chan struct{}
	var err error
	globalOtoCtx, readyChan, err = oto.NewContext(op)
	if err != nil {
		return fmt.Errorf("failed to create global oto context: %w", err)
	}

	select {
	case <-readyChan:
		log.Println("INFO: Global Oto context initialized successfully.")
	case <-time.After(otoContextTimeout):
		return fmt.Errorf("timed out waiting for oto context to be ready after %s", otoContextTimeout)
	}
	return nil
}

func playAudio(pcmData []byte) error {
	if globalOtoCtx == nil {
		return fmt.Errorf("global oto context is not initialized")
	}
	player := globalOtoCtx.NewPlayer(bytes.NewReader(pcmData))
	defer player.Close()

	player.Play()
	for player.IsPlaying() {
		time.Sleep(audioPlayPollInterval)
	}
	return nil
}
