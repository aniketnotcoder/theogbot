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
)

const telegramAPI = "https://api.telegram.org"

type Bot struct {
	token  string
	client *http.Client
	log    *slog.Logger
	offset int64
}

type update struct {
	UpdateID int64          `json:"update_id"`
	Message  *incomingMessage `json:"message"`
}

type incomingMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

func New(token string, logger *slog.Logger) *Bot {
	return &Bot{
		token: token,
		client: &http.Client{
			Timeout: 45 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 40 * time.Second,
				DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			},
		},
		log: logger,
	}
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.call(ctx, "getMe", nil, nil); err != nil {
		return fmt.Errorf("telegram getMe: %w", err)
	}
	b.log.Info("bot connected; long polling started")

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		var updates []update
		err := b.call(ctx, "getUpdates", url.Values{
			"offset":  {strconv.FormatInt(b.offset, 10)},
			"timeout": {"30"},
			"allowed_updates": { "[\"message\"]" },
		}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.log.Warn("getUpdates failed", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= b.offset {
				b.offset = u.UpdateID + 1
			}
			if u.Message != nil {
				b.handleMessage(ctx, u.Message)
			}
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *incomingMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" || msg.Chat.ID == 0 {
		return
	}
	command := strings.ToLower(strings.Fields(text)[0])
	if i := strings.IndexByte(command, '@'); i >= 0 {
		command = command[:i]
	}

	var reply string
	switch command {
	case "/start":
		reply = "Welcome to TheOGBot.\n\nA fast Telegram utility built with Go.\n\n/login — connect your Telegram account (MTProto; coming next)\n/batch — process selected accessible posts (coming next)\n/copy — copy supported accessible chat history (coming next)\n/help — show commands"
	case "/help":
		reply = "Commands:\n/start — welcome\n/login — connect your own Telegram account\n/batch — process a post link or message range\n/copy — copy supported accessible chat history\n/logout — disconnect your account\n/info — account status\n\nAccount login and content-processing commands are being implemented. Telegram permissions and protected-content rules apply."
	case "/login":
		reply = "MTProto account login is the next implementation milestone. It will use Telegram's official code and 2FA flow. Never send your login code or password to anyone."
	case "/batch":
		reply = "Batch processing is not wired yet. Send a supported Telegram post link after account login is implemented."
	case "/copy":
		reply = "Channel/group copy is not wired yet. This will recreate supported messages that your connected account can access and that Telegram permits transferring."
	case "/logout":
		reply = "No active in-memory account session is connected yet."
	case "/info":
		reply = "TheOGBot MVP\nBackend: Go\nStorage: in-memory for the initial MVP\nDatabase: none"
	default:
		reply = "Unknown command. Send /help to see available commands."
	}
	if err := b.sendMessage(ctx, msg.Chat.ID, reply); err != nil {
		b.log.Error("send message failed", "chat_id", msg.Chat.ID, "error", err)
	}
}

func (b *Bot) sendMessage(ctx context.Context, chatID int64, text string) error {
	values := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"text":    {text},
	}
	return b.call(ctx, "sendMessage", values, nil)
}

func (b *Bot) call(ctx context.Context, method string, values url.Values, out any) error {
	if values == nil {
		values = url.Values{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPI+"/bot"+b.token+"/"+method, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result apiResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.OK {
		return fmt.Errorf("telegram API %s: %s (HTTP %d)", method, result.Description, resp.StatusCode)
	}
	if out != nil && len(result.Result) > 0 {
		return json.Unmarshal(result.Result, out)
	}
	return nil
}
