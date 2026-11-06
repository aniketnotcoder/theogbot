package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aniketnotcoder/theogbot/internal/account"
)

const telegramAPI = "https://api.telegram.org"

type Bot struct {
	token string
	client *http.Client
	log *slog.Logger
	offset int64
	accounts *account.Manager
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message *incomingMessage `json:"message"`
}

type incomingMessage struct {
	MessageID int64 `json:"message_id"`
	Text string `json:"text"`
	Chat struct { ID int64 `json:"id"`; Type string `json:"type"` } `json:"chat"`
	From struct { ID int64 `json:"id"` } `json:"from"`
}

type apiResponse[T any] struct {
	OK bool `json:"ok"`
	Description string `json:"description"`
	Result T `json:"result"`
}

func New(token string, logger *slog.Logger, accounts *account.Manager) *Bot {
	return &Bot{
		token: token,
		client: &http.Client{
			Timeout: 45 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				MaxIdleConns: 100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout: 90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
				ResponseHeaderTimeout: 40 * time.Second,
				DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			},
		},
		log: logger,
		accounts: accounts,
	}
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.call(ctx, "getMe", nil, nil); err != nil {
		return fmt.Errorf("telegram getMe: %w", err)
	}
	b.log.Info("bot connected; long polling started")
	for {
		if err := ctx.Err(); err != nil { return nil }
		var updates []update
		err := b.call(ctx, "getUpdates", url.Values{
			"offset": {strconv.FormatInt(b.offset, 10)},
			"timeout": {"30"},
			"allowed_updates": { "[\"message\"]" },
		}, &updates)
		if err != nil {
			if ctx.Err() != nil { return nil }
			b.log.Warn("getUpdates failed", "error", err)
			select { case <-ctx.Done(): return nil; case <-time.After(2*time.Second): }
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= b.offset { b.offset = u.UpdateID + 1 }
			if u.Message != nil { b.handleMessage(ctx, u.Message) }
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *incomingMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" || msg.Chat.ID == 0 { return }
	fields := strings.Fields(text)
	command := strings.ToLower(fields[0])
	if i := strings.IndexByte(command, '@'); i >= 0 { command = command[:i] }

	var reply string
	if (command == "/code" || command == "/password") && msg.Chat.Type != "private" {
		_ = b.deleteMessage(ctx, msg.Chat.ID, msg.MessageID)
		_ = b.sendMessage(ctx, msg.Chat.ID, "For account security, send login codes and 2FA passwords only in a private chat with this bot.")
		return
	}
	switch command {
	case "/start":
		reply = "Welcome to TheOGBot.\n\n/login +COUNTRYCODEPHONE — connect your Telegram account\n/code CODE — submit login code\n/password PASSWORD — submit 2FA password\n/batch — batch processing (coming next)\n/copy — accessible chat copy (coming next)\n/logout — disconnect\n/info — account status"
	case "/help":
		reply = "Commands:\n/start — welcome\n/login +COUNTRYCODEPHONE — begin MTProto login\n/code CODE — submit Telegram login code\n/password PASSWORD — submit 2FA password\n/batch — process selected posts (next milestone)\n/copy — copy supported accessible history (next milestone)\n/logout — disconnect\n/info — account status\n\nUse only your own account. Never share login codes or passwords."
	case "/login":
		if len(fields) < 2 {
			reply = "Usage: /login +COUNTRYCODEPHONE\nThen send /code YOUR_CODE. If prompted, send /password YOUR_2FA_PASSWORD."
		} else if b.accounts == nil {
			reply = "MTProto unavailable. Configure API_ID and API_HASH from my.telegram.org/apps."
		} else if b.accounts.Connected(msg.From.ID) {
			reply = "An account session is already active. Use /logout first."
		} else if err := b.accounts.Begin(ctx, msg.From.ID, fields[1]); err != nil {
			reply = "Could not start login: " + err.Error()
		} else {
			reply = "Login started. Telegram will send a login code to your account. Submit it here using /code YOUR_CODE."
		}
	case "/code":
		_ = b.deleteMessage(ctx, msg.Chat.ID, msg.MessageID)
		if len(fields) != 2 {
			reply = "Usage: /code YOUR_TELEGRAM_CODE"
		} else if err := b.accounts.SubmitCode(msg.From.ID, fields[1]); err != nil {
			reply = "Could not submit code: " + err.Error()
		} else {
			reply = "Code submitted. If Telegram requests 2-step verification, send /password YOUR_PASSWORD. Check /info after authentication."
		}
	case "/password":
		_ = b.deleteMessage(ctx, msg.Chat.ID, msg.MessageID)
		password := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
		if password == "" {
			reply = "Usage: /password YOUR_2FA_PASSWORD"
		} else if err := b.accounts.SubmitPassword(msg.From.ID, password); err != nil {
			reply = "Could not submit password: " + err.Error()
		} else {
			reply = "2-step verification password submitted."
		}
	case "/batch":
		reply = "Batch processing is the next milestone. MTProto login is being implemented first."
	case "/copy":
		reply = "Channel/group copy is the next milestone. It will recreate supported messages your account can access and Telegram permits transferring."
	case "/logout":
		if b.accounts != nil && b.accounts.Logout(msg.From.ID) {
			reply = "Your in-memory Telegram session has been disconnected."
		} else { reply = "No active Telegram session." }
	case "/info":
		status := "disconnected"
		if b.accounts != nil { status = b.accounts.Status(msg.From.ID) }
		reply = fmt.Sprintf("TheOGBot MVP\nBackend: Go\nMTProto status: %s\nSession storage: memory only\nDatabase: none", status)
	default:
		reply = "Unknown command. Send /help."
	}
	if err := b.sendMessage(ctx, msg.Chat.ID, reply); err != nil {
		b.log.Error("send message failed", "chat_id", msg.Chat.ID, "error", err)
	}
}

func (b *Bot) deleteMessage(ctx context.Context, chatID, messageID int64) error {
	return b.call(ctx, "deleteMessage", url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"message_id": {strconv.FormatInt(messageID, 10)},
	}, nil)
}

func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string) error {
	return b.call(ctx, "sendMessage", url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"text": {text},
	}, nil)
}

func (b *Bot) call(ctx context.Context, method string, values url.Values, out any) error {
	if values == nil { values = url.Values{} }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPI+"/bot"+b.token+"/"+method, strings.NewReader(values.Encode()))
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	var result apiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil { return err }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.OK {
		return fmt.Errorf("telegram API %s: %s (HTTP %d)", method, result.Description, resp.StatusCode)
	}
	if out != nil && len(result.Result) > 0 { return json.Unmarshal(result.Result, out) }
	return nil
}
