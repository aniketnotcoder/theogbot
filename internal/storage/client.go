package storage

import (
	"context"
	"errors"
	"fmt"
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
		c.api, c.sender, c.uploader = api, s, u
		c.mu.Unlock()
		c.readyOnce.Do(func(){close(c.ready)})
		<-clientCtx.Done()
		return nil
	})
	c.mu.Lock()
	c.runErr = err
	c.api, c.sender, c.uploader = nil, nil, nil
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
// STORAGE_TARGET accepts a resolvable Telegram username (for example @storagegroup).
func (c *Client) UploadPath(ctx context.Context, path, filename, mime, caption string) (tg.UpdatesClass, error) {
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock()
	s, u := c.sender, c.uploader
	c.mu.RUnlock()
	file, err := u.FromPath(ctx, path)
	if err != nil { return nil, fmt.Errorf("upload file: %w", err) }
	doc := message.UploadedDocument(file).Filename(filename).MIME(mime)
	_ = caption // Caption styling is added with the message-metadata phase.
	return s.Resolve(c.target).Media(ctx, doc)
}
