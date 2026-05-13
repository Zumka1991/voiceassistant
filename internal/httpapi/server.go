package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"voiceassistant/internal/config"
	"voiceassistant/internal/llm"
	"voiceassistant/internal/stt"
	"voiceassistant/internal/tts"
)

type Server struct {
	cfg *config.Config
	llm *llm.Client
	stt *stt.Client
	tts *tts.Client
}

func New(cfg *config.Config, llmClient *llm.Client, sttClient *stt.Client, ttsClient *tts.Client) *Server {
	return &Server{cfg: cfg, llm: llmClient, stt: sttClient, tts: ttsClient}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir("web")))
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/chat", s.chat)
	mux.HandleFunc("/api/voice", s.voice)
	mux.HandleFunc("/ws/voice", s.voiceWS)

	return logMiddleware(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"model":       s.cfg.LLMModel,
		"voiceReady":  s.cfg.YandexAPIKey != "" && s.cfg.YandexFolderID != "",
	})
}

type chatRequest struct {
	Message string        `json:"message"`
	History []llm.Message `json:"history,omitempty"`
}

type chatResponse struct {
	Reply string `json:"reply"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is empty", http.StatusBadRequest)
		return
	}

	messages := make([]llm.Message, 0, len(req.History)+2)
	messages = append(messages, llm.Message{Role: "system", Content: s.cfg.SystemPrompt})
	messages = append(messages, req.History...)
	messages = append(messages, llm.Message{Role: "user", Content: req.Message})

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	reply, err := s.llm.Chat(ctx, messages)
	if err != nil {
		log.Printf("llm error: %v", err)
		http.Error(w, "llm error: "+err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, chatResponse{Reply: reply})
}

// voice — принимает raw LPCM 16-bit 16kHz mono в теле, возвращает audio/ogg.
// Заголовки X-Recognized-Text и X-Reply-Text несут текст распознавания и ответа LLM
// (закодированы в URL-кодировке для безопасной передачи юникода).
func (s *Server) voice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pcm, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20)) // до 5 МБ ≈ 2.5 мин 16k 16bit
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(pcm) < 1000 {
		http.Error(w, "audio too short", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// 1) STT
	text, err := s.stt.RecognizeLPCM(ctx, pcm)
	if err != nil {
		log.Printf("stt error: %v", err)
		http.Error(w, "stt error: "+err.Error(), http.StatusBadGateway)
		return
	}
	if strings.TrimSpace(text) == "" {
		http.Error(w, "speech not recognized (тишина или шум)", http.StatusUnprocessableEntity)
		return
	}
	log.Printf("STT: %q", text)

	// 2) LLM
	history := parseHistory(r.Header.Get("X-History"))
	messages := make([]llm.Message, 0, len(history)+2)
	messages = append(messages, llm.Message{Role: "system", Content: s.cfg.SystemPrompt})
	messages = append(messages, history...)
	messages = append(messages, llm.Message{Role: "user", Content: text})

	reply, err := s.llm.Chat(ctx, messages)
	if err != nil {
		log.Printf("llm error: %v", err)
		http.Error(w, "llm error: "+err.Error(), http.StatusBadGateway)
		return
	}
	log.Printf("LLM: %q", reply)

	// 3) TTS
	audio, contentType, err := s.tts.Synthesize(ctx, reply)
	if err != nil {
		log.Printf("tts error: %v", err)
		http.Error(w, "tts error: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Recognized-Text", urlEncode(text))
	w.Header().Set("X-Reply-Text", urlEncode(reply))
	w.Header().Set("Access-Control-Expose-Headers", "X-Recognized-Text, X-Reply-Text")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

func parseHistory(s string) []llm.Message {
	if s == "" {
		return nil
	}
	if decoded, err := url.QueryUnescape(s); err == nil {
		s = decoded
	}
	var out []llm.Message
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// urlEncode — простое кодирование для HTTP-заголовков (HTTP-заголовки должны быть ASCII).
func urlEncode(s string) string {
	// percent-encoding всего, кроме безопасных байт
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0F])
		}
	}
	return b.String()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
