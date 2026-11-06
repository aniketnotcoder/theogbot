package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// Client is a bot-authenticated MTProto uploader, separate from user source sessions.
type Client struct {
	appID int
	appHash, token, target string
	mu sync.RWMutex
	api *tg.Client
	sender *message.Sender
	uploader *uploader.Uploader
	targetPeer tg.InputPeerClass
	ready chan struct{}
	readyOnce sync.Once
	runErr error
}

func New(appID int, appHash, token, target string) (*Client, error) {
	if appID <= 0 || appHash == "" || token == "" || target == "" {
		return nil, errors.New("API_ID, API_HASH, BOT_TOKEN and STORAGE_TARGET are required")
	}
	return &Client{appID:appID, appHash:appHash, token:token, target:target, ready:make(chan struct{})}, nil
}

func (c *Client) Run(ctx context.Context) error {
	client := telegram.NewClient(c.appID, c.appHash, telegram.Options{SessionStorage:&session.StorageMemory{}, NoUpdates:true})
	err := client.Run(ctx, func(clientCtx context.Context) error {
		if _, err := client.Auth().Bot(clientCtx, c.token); err != nil { return fmt.Errorf("authenticate storage bot: %w", err) }
		api := tg.NewClient(client)
		u := uploader.NewUploader(api).WithThreads(4)
		s := message.NewSender(api).WithUploader(u)
		c.mu.Lock()
		var peer tg.InputPeerClass
		if strings.HasPrefix(strings.TrimSpace(c.target), "-") {
			var resolveErr error
			peer, resolveErr = resolveTarget(clientCtx, api, c.target)
			if resolveErr != nil { return fmt.Errorf("resolve STORAGE_TARGET: %w", resolveErr) }
		}
		c.api, c.sender, c.uploader, c.targetPeer = api, s, u, peer
		c.mu.Unlock()
		c.readyOnce.Do(func(){close(c.ready)})
		<-clientCtx.Done()
		return nil
	})
	c.mu.Lock()
	c.runErr = err
	c.api, c.sender, c.uploader, c.targetPeer = nil, nil, nil, nil
	c.mu.Unlock()
	c.readyOnce.Do(func(){close(c.ready)})
	return err
}

func (c *Client) Ready(ctx context.Context) error {
	select {
	case <-ctx.Done(): return ctx.Err()
	case <-c.ready:
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.api == nil { if c.runErr != nil { return c.runErr }; return errors.New("storage bot is not connected") }
		return nil
	}
}

// UploadPath posts a new document message to the configured destination.
// STORAGE_TARGET accepts a public username or a private numeric chat ID (-100... for channels/supergroups).
func (c *Client) UploadPath(ctx context.Context, path, filename, mime, caption string) (tg.UpdatesClass, error) {
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock()
	s, u, peer := c.sender, c.uploader, c.targetPeer
	c.mu.RUnlock()
	file, err := u.FromPath(ctx, path)
	if err != nil { return nil, fmt.Errorf("upload file: %w", err) }
	doc := message.UploadedDocument(file).Filename(filename).MIME(mime)
	_ = caption // Caption styling is added with the message-metadata phase.
	if peer != nil { return s.To(peer).Media(ctx, doc) }
	return s.Resolve(c.target).Media(ctx, doc)
}


// resolveTarget supports public usernames and private groups/channels already
// visible in the storage bot's dialogs. Numeric targets use Telegram's Bot API
// chat ID form (-100<channel id> for supergroups/channels, -<chat id> for groups).
func resolveTarget(ctx context.Context, api *tg.Client, target string) (tg.InputPeerClass, error) {
	target = strings.TrimSpace(target)
	if target == "" { return nil, errors.New("STORAGE_TARGET is empty") }
	if !strings.HasPrefix(target, "-") {
		return nil, fmt.Errorf("private numeric storage target required; public usernames are resolved by the sender at upload time")
	}
	id, err := strconv.ParseInt(target, 10, 64)
	if err != nil { return nil, fmt.Errorf("invalid numeric chat ID %q", target) }
	result, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer:&tg.InputPeerEmpty{}, Limit:200, Hash:0})
	if err != nil { return nil, fmt.Errorf("read storage bot dialogs: %w", err) }
	var chats []tg.ChatClass
	switch v := result.(type) {
	case *tg.MessagesDialogs: chats = v.Chats
	case *tg.MessagesDialogsSlice: chats = v.Chats
	default: return nil, fmt.Errorf("unsupported dialogs response %T", result)
	}
	if id <= -1000000000000 {
		channelID := -id - 1000000000000
		for _, item := range chats { if ch, ok := item.(*tg.Channel); ok && int64(ch.ID)==channelID {
			if ch.AccessHash==0 { return nil, errors.New("storage channel access hash unavailable") }
			return &tg.InputPeerChannel{ChannelID:ch.ID,AccessHash:ch.AccessHash},nil
		} }
	} else if id < 0 {
		chatID := -id
		for _, item := range chats { if ch, ok := item.(*tg.Chat); ok && int64(ch.ID)==chatID { return &tg.InputPeerChat{ChatID:ch.ID},nil } }
	}
	return nil, fmt.Errorf("storage chat %s was not found in the bot's dialogs; add the bot to that group/channel first", target)
}
