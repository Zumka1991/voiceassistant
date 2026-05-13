package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port             string
	OpenRouterAPIKey string
	LLMModel         string
	SystemPrompt     string
	YandexAPIKey     string
	YandexFolderID   string
	YandexVoice      string
	YandexEmotion    string
	YandexSpeed      string
}

func Load() (*Config, error) {
	_ = loadDotEnv(".env")

	c := &Config{
		Port:             getenv("PORT", "8080"),
		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		LLMModel:         getenv("LLM_MODEL", "google/gemini-2.0-flash-001"),
		SystemPrompt:     getenv("SYSTEM_PROMPT", "Ты дружелюбный голосовой ассистент. Отвечай кратко, на русском языке."),
		YandexAPIKey:     os.Getenv("YANDEX_API_KEY"),
		YandexFolderID:   os.Getenv("YANDEX_FOLDER_ID"),
		YandexVoice:      getenv("YANDEX_VOICE", "alena"),
		YandexEmotion:    getenv("YANDEX_EMOTION", "good"),
		YandexSpeed:      getenv("YANDEX_SPEED", "1.0"),
	}

	if c.OpenRouterAPIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY не задан (см. .env.example)")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv — простой парсер .env без сторонних зависимостей.
// Поддерживает KEY=VALUE, комментарии (#), значения в одинарных/двойных кавычках.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return s.Err()
}
