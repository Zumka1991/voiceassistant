package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"voiceassistant/internal/llm"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true }, // dev only
}

// Протокол WS:
//   client → server: text JSON {"type":"start","history":[...]}
//                    binary: PCM int16 LE 16kHz mono чанки
//                    text JSON {"type":"end"}
//   server → client: text JSON {"type":"recognized","text":"..."}
//                    text JSON {"type":"reply_chunk","text":"..."}
//                    binary: OggOpus аудио (одно предложение = один блоб)
//                    text JSON {"type":"done"} | {"type":"error","message":"..."}

type wsInbound struct {
	Type    string        `json:"type"`
	History []llm.Message `json:"history"`
}

type wsOutbound struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *Server) voiceWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(8 << 20) // 8 МБ на сообщение PCM
	conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(2 * time.Minute)); return nil })

	// Сериализуем запись (TTS-горутины пишут параллельно).
	var writeMu sync.Mutex
	sendJSON := func(v wsOutbound) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}
	sendBinary := func(b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.BinaryMessage, b)
	}

	for {
		// Один полный «turn»: ждём start, копим PCM до end, обрабатываем.
		if err := handleTurn(r.Context(), conn, s, sendJSON, sendBinary); err != nil {
			if !isClosedErr(err) {
				log.Printf("ws turn: %v", err)
				_ = sendJSON(wsOutbound{Type: "error", Message: err.Error()})
			}
			return
		}
	}
}

func handleTurn(parentCtx context.Context, conn *websocket.Conn, s *Server,
	sendJSON func(wsOutbound) error, sendBinary func([]byte) error) error {

	var history []llm.Message
	var pcm []byte
	started := false

	for {
		conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		switch mt {
		case websocket.TextMessage:
			var in wsInbound
			if err := json.Unmarshal(data, &in); err != nil {
				return err
			}
			switch in.Type {
			case "start":
				history = in.History
				pcm = pcm[:0]
				started = true
			case "end":
				if !started {
					return nil
				}
				return processTurn(parentCtx, s, history, pcm, sendJSON, sendBinary)
			}
		case websocket.BinaryMessage:
			if !started {
				continue
			}
			pcm = append(pcm, data...)
		}
	}
}

func processTurn(parentCtx context.Context, s *Server, history []llm.Message, pcm []byte,
	sendJSON func(wsOutbound) error, sendBinary func([]byte) error) error {

	ctx, cancel := context.WithTimeout(parentCtx, 60*time.Second)
	defer cancel()

	turnStart := time.Now()
	log.Printf("WS turn: pcm=%d bytes (~%.1f sec)", len(pcm), float64(len(pcm))/(16000.0*2))
	if len(pcm) < 1000 {
		return sendJSON(wsOutbound{Type: "error", Message: "audio too short"})
	}

	// 1) STT
	sttStart := time.Now()
	text, err := s.stt.RecognizeLPCM(ctx, pcm)
	if err != nil {
		log.Printf("WS STT err: %v", err)
		return sendJSON(wsOutbound{Type: "error", Message: "stt: " + err.Error()})
	}
	if strings.TrimSpace(text) == "" {
		log.Printf("WS STT: empty (тишина/шум) %v", time.Since(sttStart))
		return sendJSON(wsOutbound{Type: "error", Message: "не распознано (тишина или шум)"})
	}
	log.Printf("WS STT (%v): %q", time.Since(sttStart).Round(time.Millisecond), text)
	if err := sendJSON(wsOutbound{Type: "recognized", Text: text}); err != nil {
		return err
	}

	// 2) LLM stream → накапливаем по предложениям → 3) TTS параллельно
	messages := make([]llm.Message, 0, len(history)+2)
	messages = append(messages, llm.Message{Role: "system", Content: s.cfg.SystemPrompt})
	messages = append(messages, history...)
	messages = append(messages, llm.Message{Role: "user", Content: text})

	var (
		buf strings.Builder
		wg  sync.WaitGroup
	)

	firstChunkAt := time.Time{}
	flushSentence := func(sentence string) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			return
		}
		if firstChunkAt.IsZero() {
			firstChunkAt = time.Now()
			log.Printf("WS first sentence ready at +%v: %q", time.Since(turnStart).Round(time.Millisecond), sentence)
		}
		_ = sendJSON(wsOutbound{Type: "reply_chunk", Text: sentence})
		wg.Add(1)
		go func() {
			defer wg.Done()
			ttsStart := time.Now()
			audio, _, err := s.tts.Synthesize(ctx, sentence)
			if err != nil {
				log.Printf("tts chunk: %v", err)
				_ = sendJSON(wsOutbound{Type: "error", Message: "tts: " + err.Error()})
				return
			}
			log.Printf("WS TTS (%v, %d bytes): %q", time.Since(ttsStart).Round(time.Millisecond), len(audio), sentence)
			_ = sendBinary(audio)
		}()
	}

	// Параллельный TTS — сохраним порядок: TTS-горутины запускаются последовательно
	// flushSentence-ом, и каждый sendBinary под общим mutex'ом — соблюдает порядок входа.
	full, err := s.llm.ChatStream(ctx, messages, func(delta string) {
		buf.WriteString(delta)
		s := buf.String()
		for {
			idx := sentenceEnd(s)
			if idx < 0 {
				break
			}
			flushSentence(s[:idx+1])
			s = s[idx+1:]
			buf.Reset()
			buf.WriteString(s)
		}
	})
	if err != nil {
		return sendJSON(wsOutbound{Type: "error", Message: "llm: " + err.Error()})
	}
	// хвост без знака конца
	if rest := strings.TrimSpace(buf.String()); rest != "" {
		flushSentence(rest)
	}
	_ = full

	wg.Wait()
	log.Printf("WS turn done in %v", time.Since(turnStart).Round(time.Millisecond))
	return sendJSON(wsOutbound{Type: "done"})
}

// sentenceEnd возвращает байтовый индекс символа конца предложения (. ! ? …),
// после которого идёт пробел/конец строки — иначе -1.
func sentenceEnd(s string) int {
	last := -1
	prevWasTerm := false
	prevTermIdx := -1
	for i, r := range s {
		if prevWasTerm {
			if unicode.IsSpace(r) {
				return prevTermIdx
			}
			prevWasTerm = false
		}
		if r == '.' || r == '!' || r == '?' || r == '…' {
			prevWasTerm = true
			prevTermIdx = i
		} else if r == '\n' {
			return i
		}
		last = i
	}
	if prevWasTerm && prevTermIdx == last {
		// конец строки — границу считать не будем, ждём ещё токенов
	}
	return -1
}

func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) ||
		strings.Contains(err.Error(), "use of closed network connection")
}
