package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Yandex SpeechKit v1 — короткое распознавание (до ~30 сек, ≤1 МБ).
// Документация: https://yandex.cloud/ru/docs/speechkit/stt/api/request-api
const sttURL = "https://stt.api.cloud.yandex.net/speech/v1/stt:recognize"

type Client struct {
	apiKey   string
	folderID string
	http     *http.Client
}

func New(apiKey, folderID string) *Client {
	return &Client{
		apiKey:   apiKey,
		folderID: folderID,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

type recognizeResponse struct {
	Result   string `json:"result"`
	ErrorCode string `json:"error_code,omitempty"`
	ErrorMsg  string `json:"error_message,omitempty"`
}

// RecognizeLPCM — принимает сырой PCM (16-bit signed little-endian, моно, 16 kHz).
func (c *Client) RecognizeLPCM(ctx context.Context, pcm []byte) (string, error) {
	if c.apiKey == "" || c.folderID == "" {
		return "", fmt.Errorf("YANDEX_API_KEY или YANDEX_FOLDER_ID не задан")
	}

	q := url.Values{}
	q.Set("folderId", c.folderID)
	q.Set("lang", "ru-RU")
	q.Set("format", "lpcm")
	q.Set("sampleRateHertz", "16000")
	q.Set("topic", "general")

	req, err := http.NewRequestWithContext(ctx, "POST", sttURL+"?"+q.Encode(), bytes.NewReader(pcm))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("yandex stt %d: %s", resp.StatusCode, string(raw))
	}

	var out recognizeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode stt: %w (body=%s)", err, string(raw))
	}
	if out.ErrorCode != "" {
		return "", fmt.Errorf("yandex stt: %s — %s", out.ErrorCode, out.ErrorMsg)
	}
	return out.Result, nil
}
