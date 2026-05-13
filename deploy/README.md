# Деплой на сервер (тестовый, на порту 8443)

## Условия

- 80/443 на сервере занят другим сайтом — мы их не трогаем
- Слушаем HTTPS на **8443** (самоподписанный сертификат, браузер один раз попросит «Принять риск»)
- Для микрофона достаточно HTTPS-контекста — самоподписанный сертификат подходит

## Шаги

```bash
# 1) на сервере: установить Docker (если ещё не стоит)
curl -fsSL https://get.docker.com | sh

# 2) открыть порт 8443 в firewall
ufw allow 8443/tcp     # или соответствующая команда для твоего firewall

# 3) клонировать репо
git clone https://github.com/Zumka1991/voiceassistant /opt/voiceassistant
cd /opt/voiceassistant

# 4) создать .env с ключами
cp .env.example .env
nano .env       # заполнить OPENROUTER_API_KEY, YANDEX_API_KEY, YANDEX_FOLDER_ID

# 5) (опционально) добавить A-запись DNS:
#    voice.gendoctor.ru -> <IP сервера>
#    Если домена нет — можно ходить по IP:8443, но придётся ещё раз согласиться на сертификат.

# 6) запустить
cd deploy
docker compose up -d --build
docker compose logs -f voice-assistant
```

## Проверка

Открой в браузере: `https://voice.gendoctor.ru:8443` (или `https://<IP>:8443`).
Браузер покажет страницу «Подключение не защищено» — это нормально для самоподписанного сертификата. Жми «Дополнительно» → «Перейти на сайт».

Дальше всё как локально: «📞 Позвонить», говори.

## Обновление

```bash
cd /opt/voiceassistant
git pull
cd deploy && docker compose up -d --build
```

## Если позже захочешь нормальный HTTPS (без warning)

Варианты:
1. **Reverse-proxy через тот сервер, что обслуживает gendoctor.ru** (nginx/Caddy на 443) — добавить там сабдомен `voice.gendoctor.ru` с `proxy_pass` на наш контейнер. В этом случае Caddy в deploy/ не нужен — оставляем только сервис `voice-assistant`, прокидывая порт `127.0.0.1:8080:8080`.
2. **Let's Encrypt с DNS-01 challenge** — Caddy умеет, но нужен плагин под твоего DNS-провайдера.
