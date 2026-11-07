package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

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

const startupTimeout = 20 * time.Second

func New(appID int, appHash, token, target string) (*Client, error) {
	if appID <= 0 || appHash == "" || token == "" { return nil, errors.New("API_ID, API_HASH and BOT_TOKEN are required") }
	return &Client{appID:appID, appHash:appHash, token:token, target:target, ready:make(chan struct{})}, nil
}

func (c *Client) Run(ctx context.Context) error {
	client := telegram.NewClient(c.appID, c.appHash, telegram.Options{SessionStorage:&session.StorageMemory{}, NoUpdates:true})
	err := client.Run(ctx, func(clientCtx context.Context) error {
		startupCtx, cancelStartup := context.WithTimeout(clientCtx, startupTimeout)
		defer cancelStartup()
		if _, err := client.Auth().Bot(startupCtx, c.token); err != nil { return fmt.Errorf("authenticate storage bot: %w", err) }
		api := tg.NewClient(client)
		u := uploader.NewUploader(api).WithThreads(4)
		s := message.NewSender(api).WithUploader(u)
		var peer tg.InputPeerClass
		if strings.HasPrefix(strings.TrimSpace(c.target), "-") {
			var resolveErr error
			peer, resolveErr = resolveTarget(startupCtx, api, c.target)
			if resolveErr != nil { return fmt.Errorf("resolve STORAGE_TARGET: %w", resolveErr) }
		}
		c.mu.Lock()
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

func (c *Client) Target() string { return c.target }

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

func (c *Client) UploadPath(ctx context.Context, path, filename, mime, caption, kind string, attrs []tg.DocumentAttributeClass, entities []tg.MessageEntityClass) (tg.UpdatesClass, error) {
	return c.UploadPathTo(ctx, nil, path, filename, mime, caption, kind, attrs, nil)
}

func (c *Client) UploadPathTo(ctx context.Context, destination tg.InputPeerClass, path, filename, mime, caption, kind string, attrs []tg.DocumentAttributeClass) (tg.UpdatesClass, error) {
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock()
	s, u, configuredPeer := c.sender, c.uploader, c.targetPeer
	peer := destination
	if peer == nil { peer = configuredPeer }
	c.mu.RUnlock()
	file, err := u.FromPath(ctx, path)
	if err != nil { return nil, fmt.Errorf("upload file: %w", err) }
	captionOption := captionOptions(caption, entities)
	if kind == "photo" {
		if peer != nil { return s.To(peer).UploadedPhoto(ctx, file, captionOption...) }
		return s.Resolve(c.target).UploadedPhoto(ctx, file, captionOption...)
	}
	preserved := make([]tg.DocumentAttributeClass, 0, len(attrs))
	for _, attr := range attrs { if _, isFilename := attr.(*tg.DocumentAttributeFilename); !isFilename { preserved = append(preserved, attr) } }
	doc := message.UploadedDocument(file, captionOption...).Filename(filename).MIME(mime).Attributes(preserved...)
	if peer != nil { return s.To(peer).Media(ctx, doc) }
	return s.Resolve(c.target).Media(ctx, doc)
}

type AlbumItem struct {
	Path, Filename, MIME, Kind, Caption string
	Attributes []tg.DocumentAttributeClass
	Entities []tg.MessageEntityClass
}

// UploadAlbumTo uploads a Telegram media group (2-10 photos/videos) and sends it
// as one grouped message. Other document types must use UploadPathTo individually.
func (c *Client) UploadAlbumTo(ctx context.Context, destination tg.InputPeerClass, items []AlbumItem) (tg.UpdatesClass, error) {
	if len(items) < 2 || len(items) > 10 { return nil, fmt.Errorf("album must contain between 2 and 10 items") }
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock()
	s, u, configuredPeer := c.sender, c.uploader, c.targetPeer
	peer := destination
	if peer == nil { peer = configuredPeer }
	c.mu.RUnlock()
	if peer == nil && strings.TrimSpace(c.target) == "" { return nil, errors.New("album destination is required") }
	options := make([]message.MultiMediaOption, 0, len(items))
	for i, item := range items {
		if item.Kind != "photo" && item.Kind != "video" { return nil, fmt.Errorf("album item %d has unsupported kind %q; only photos and videos can be grouped", i+1, item.Kind) }
		file, err := u.FromPath(ctx, item.Path)
		if err != nil { return nil, fmt.Errorf("upload album item %d: %w", i+1, err) }
		caption := []styling.StyledTextOption{}
		caption = captionOptions(item.Caption, item.Entities)
		var option message.MediaOption
		if item.Kind == "photo" {
			option = message.UploadedPhoto(file, caption...)
		} else {
			preserved := make([]tg.DocumentAttributeClass, 0, len(item.Attributes))
			for _, attr := range item.Attributes { if _, isFilename := attr.(*tg.DocumentAttributeFilename); !isFilename { preserved = append(preserved, attr) } }
			option = message.UploadedDocument(file, caption...).Filename(item.Filename).MIME(item.MIME).Attributes(preserved...)
		}
		options = append(options, message.ForceMulti(option))
	}
	builder := s.To(peer)
	return builder.Album(ctx, options[0], options[1:]...)
}

func (c *Client) SendText(ctx context.Context, text string) (tg.UpdatesClass, error) {
	return c.SendTextTo(ctx, nil, text)
}

func (c *Client) SendTextTo(ctx context.Context, destination tg.InputPeerClass, text string) (tg.UpdatesClass, error) {
	if strings.TrimSpace(text) == "" { return nil, errors.New("text message is empty") }
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock()
	s, configuredPeer := c.sender, c.targetPeer
	peer := destination
	if peer == nil { peer = configuredPeer }
	c.mu.RUnlock()
	if peer != nil { return s.To(peer).Text(ctx, text) }
	if strings.TrimSpace(c.target) == "" { return nil, errors.New("text destination is required") }
	return s.Resolve(c.target).Text(ctx, text)
}

func (c *Client) ResolvePeer(ctx context.Context, target string) (tg.InputPeerClass, error) {
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock(); api := c.api; c.mu.RUnlock()
	return resolveTarget(ctx, api, target)
}

func resolveTarget(ctx context.Context, api *tg.Client, target string) (tg.InputPeerClass, error) {
	target = strings.TrimSpace(target)
	if target == "" { return nil, errors.New("STORAGE_TARGET is empty") }
	if !strings.HasPrefix(target, "-") { return nil, fmt.Errorf("private numeric storage target required; public usernames are resolved by the sender at upload time") }
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

func captionOptions(caption string, entities []tg.MessageEntityClass) []styling.StyledTextOption {
	if caption == "" { return nil }
	if len(entities) == 0 { return []styling.StyledTextOption{styling.Plain(caption)} }
	return []styling.StyledTextOption{styling.Custom(func(builder *entity.Builder) error {
		type span struct { start, end int; formatter entity.Formatter }
		spans := make([]span, 0, len(entities))
		for _, source := range entities {
			if source == nil || source.GetOffset() < 0 || source.GetLength() <= 0 { continue }
			formatter := captionFormatter(source)
			if formatter == nil { continue }
			start, okStart := utf16ByteOffset(caption, source.GetOffset())
			end, okEnd := utf16ByteOffset(caption, source.GetOffset()+source.GetLength())
			if !okStart || !okEnd || start >= end { continue }
			spans = append(spans, span{start:start, end:end, formatter:formatter})
		}
		boundaries := []int{0, len(caption)}
		for _, item := range spans { boundaries = append(boundaries, item.start, item.end) }
		sort.Ints(boundaries)
		unique := boundaries[:0]
		for _, point := range boundaries { if len(unique)==0 || unique[len(unique)-1]!=point { unique=append(unique,point) } }
		for i:=0; i+1<len(unique); i++ {
			start,end:=unique[i],unique[i+1]
			if start==end { continue }
			formats:=make([]entity.Formatter,0,2)
			for _, item:=range spans { if item.start<=start && item.end>=end { formats=append(formats,item.formatter) } }
			builder.Format(caption[start:end],formats...)
		}
		return nil
	})}
}

func utf16ByteOffset(value string, target int) (int, bool) {
	if target < 0 { return 0, false }
	units, bytes := 0, 0
	if target == 0 { return 0, true }
	for _, r := range value {
		width := 1
		if r > 0xFFFF { width = 2 }
		if units+width > target { return 0, false }
		units += width
		bytes += len(string(r))
		if units == target { return bytes, true }
	}
	return 0, units == target
}

func captionFormatter(source tg.MessageEntityClass) entity.Formatter {
	switch item := source.(type) {
	case *tg.MessageEntityBold: return entity.Bold()
	case *tg.MessageEntityItalic: return entity.Italic()
	case *tg.MessageEntityUnderline: return entity.Underline()
	case *tg.MessageEntityStrike: return entity.Strike()
	case *tg.MessageEntityCode: return entity.Code()
	case *tg.MessageEntityPre: return entity.Pre(item.Language)
	case *tg.MessageEntityURL: return entity.URL()
	case *tg.MessageEntityTextURL: return entity.TextURL(item.URL)
	case *tg.MessageEntityMention: return entity.Mention()
	case *tg.MessageEntityHashtag: return entity.Hashtag()
	case *tg.MessageEntityBotCommand: return entity.BotCommand()
	case *tg.MessageEntityEmail: return entity.Email()
	case *tg.MessageEntityPhone: return entity.Phone()
	case *tg.MessageEntityCashtag: return entity.Cashtag()
	case *tg.MessageEntityBankCard: return entity.BankCard()
	case *tg.MessageEntitySpoiler: return entity.Spoiler()
	case *tg.MessageEntityCustomEmoji: return entity.CustomEmoji(item.DocumentID)
	case *tg.MessageEntityBlockquote: return entity.Blockquote(item.Collapsed)
	case *tg.InputMessageEntityMentionName: return entity.MentionName(item.UserID)
	default: return nil
	}
}
