package tts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Yandex SpeechKit v1 — синтез речи.
// https://yandex.cloud/ru/docs/speechkit/tts/request
const ttsURL = "https://tts.api.cloud.yandex.net/speech/v1/tts:synthesize"

type Client struct {
	apiKey   string
	folderID string
	voice    string // alena, filipp, jane, omazh, zahar, ermil, madirus, marina, lera, dasha
	emotion  string // good, neutral, evil — не у всех голосов
	speed    string // 0.1 .. 3.0
	http     *http.Client
}

func New(apiKey, folderID, voice, emotion, speed string) *Client {
	if voice == "" {
		voice = "alena"
	}
	if speed == "" {
		speed = "1.0"
	}
	return &Client{
		apiKey:   apiKey,
		folderID: folderID,
		voice:    voice,
		emotion:  emotion,
		speed:    speed,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Synthesize — возвращает аудио в формате OggOpus (готов к воспроизведению в браузере).
func (c *Client) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if c.apiKey == "" || c.folderID == "" {
		return nil, "", fmt.Errorf("YANDEX_API_KEY или YANDEX_FOLDER_ID не задан")
	}

	form := url.Values{}
	form.Set("text", text)
	form.Set("lang", "ru-RU")
	form.Set("voice", c.voice)
	form.Set("folderId", c.folderID)
	form.Set("format", "oggopus")
	form.Set("speed", c.speed)
	if c.emotion != "" {
		form.Set("emotion", c.emotion)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ttsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("yandex tts %d: %s", resp.StatusCode, string(body))
	}
	return body, "audio/ogg", nil
}
