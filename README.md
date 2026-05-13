# VoiceAssistant

Голосовой ИИ-ассистент на Go. Этап 1 — текстовый чат через OpenRouter и тестовая веб-морда.
Далее будет добавлено: Yandex SpeechKit (STT/TTS), стриминг, голосовой пайплайн в браузере, телефония.

## Структура

```
cmd/server/         точка входа
internal/config/    загрузка .env и переменных окружения
internal/llm/       клиент OpenRouter
internal/httpapi/   HTTP-хендлеры (/api/chat, /api/voice — заглушка)
internal/stt/       Yandex SpeechKit STT (TODO)
internal/tts/       Yandex SpeechKit TTS (TODO)
web/                тестовая веб-морда (HTML+JS)
```

## Запуск локально

1. Установить Go 1.23+
2. Скопировать `.env.example` в `.env`, заполнить `OPENROUTER_API_KEY`
3. `go run ./cmd/server`
4. Открыть http://localhost:8080

## Запуск в Docker

```
docker compose up --build
```

## Переменные окружения

| Переменная           | По умолчанию                       | Описание                              |
|----------------------|------------------------------------|---------------------------------------|
| `PORT`               | `8080`                             | Порт HTTP-сервера                     |
| `OPENROUTER_API_KEY` | —                                  | Обязательно                           |
| `LLM_MODEL`          | `google/gemini-2.0-flash-001`      | Любая модель OpenRouter               |
| `SYSTEM_PROMPT`      | (см. `.env.example`)               | Системный промпт ассистента           |
| `YANDEX_API_KEY`     | —                                  | Для STT/TTS (этап 2)                  |
| `YANDEX_FOLDER_ID`   | —                                  | Yandex Cloud folder ID                |

## API

- `GET  /`            — веб-морда
- `GET  /api/health`  — `{status, model}`
- `POST /api/chat`    — `{message, history?}` → `{reply}`
- `WS   /api/voice`   — пока заглушка (501)
