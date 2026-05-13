package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"voiceassistant/internal/config"
	"voiceassistant/internal/httpapi"
	"voiceassistant/internal/llm"
	"voiceassistant/internal/stt"
	"voiceassistant/internal/tts"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	llmClient := llm.New(cfg.OpenRouterAPIKey, cfg.LLMModel)
	sttClient := stt.New(cfg.YandexAPIKey, cfg.YandexFolderID)
	ttsClient := tts.New(cfg.YandexAPIKey, cfg.YandexFolderID, cfg.YandexVoice, cfg.YandexEmotion, cfg.YandexSpeed)
	srv := httpapi.New(cfg, llmClient, sttClient, ttsClient)

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("VoiceAssistant запущен на http://localhost:%s (модель: %s)", cfg.Port, cfg.LLMModel)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("остановка...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}
