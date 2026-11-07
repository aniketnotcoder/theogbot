package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"os"
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
	"github.com/gotd/td/tgerr"
)

type TransferProgress func(current, total int64)

// Telegram allows upload parts up to 512 KiB. gotd v0.130 defaults to
// 128 KiB, which can exceed server part-count limits on large files.
const uploadThreads = 8
const uploadPartSize = uploader.MaximumPartSize

func newTransferUploader(api *tg.Client) *uploader.Uploader {
	return uploader.NewUploader(api).WithPartSize(uploadPartSize).WithThreads(uploadThreads)
}

type uploadProgress struct { mu sync.Mutex; uploaded map[int64]int64; total int64; callback TransferProgress }
func (p *uploadProgress) Chunk(_ context.Context, state uploader.ProgressState) error {
	p.mu.Lock(); if p.uploaded == nil { p.uploaded = make(map[int64]int64) }; p.uploaded[state.ID] = state.Uploaded
	var current int64; for _, n := range p.uploaded { current += n }; total := p.total; cb := p.callback; p.mu.Unlock()
	if cb != nil { cb(current,total) }; return nil
}

func retryFloodWait(ctx context.Context, operation func() (tg.UpdatesClass,error)) (tg.UpdatesClass,error) {
	const maxAttempts = 6
	for attempt:=0; attempt<maxAttempts; attempt++ {
		result, err := operation(); if err == nil { return result,nil }
		flood, waitErr := tgerr.FloodWait(ctx,err)
		if flood { continue }; if waitErr != nil { return nil,waitErr }; return nil,err
	}
	return nil, fmt.Errorf("Telegram FloodWait persisted after %d safe retries",maxAttempts)
}

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
		u := newTransferUploader(api)
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
	return c.UploadPathTo(ctx, nil, path, filename, mime, caption, kind, attrs, entities)
}

func (c *Client) UploadPathTo(ctx context.Context, destination tg.InputPeerClass, path, filename, mime, caption, kind string, attrs []tg.DocumentAttributeClass, entities []tg.MessageEntityClass) (tg.UpdatesClass, error) {
	return c.UploadPathToWithProgress(ctx,destination,path,filename,mime,caption,kind,attrs,entities,nil)
}

func (c *Client) UploadPathToWithProgress(ctx context.Context, destination tg.InputPeerClass, path, filename, mime, caption, kind string, attrs []tg.DocumentAttributeClass, entities []tg.MessageEntityClass, progress TransferProgress) (tg.UpdatesClass,error) {
	if err:=c.Ready(ctx);err!=nil{return nil,err}
	c.mu.RLock(); api,configuredPeer:=c.api,c.targetPeer; peer:=destination; if peer==nil{peer=configuredPeer}; target:=c.target; c.mu.RUnlock()
	info,err:=os.Stat(path);if err!=nil{return nil,fmt.Errorf("stat upload file: %w",err)}
	return retryFloodWait(ctx,func()(tg.UpdatesClass,error){
		u:=newTransferUploader(api).WithProgress(&uploadProgress{total:info.Size(),callback:progress})
		s:=message.NewSender(api).WithUploader(u)
		file,err:=u.FromPath(ctx,path);if err!=nil{return nil,fmt.Errorf("upload file: %w",err)}
		captionOption:=captionOptions(caption,entities)
		if kind=="photo"{if peer!=nil{return s.To(peer).UploadedPhoto(ctx,file,captionOption...)};return s.Resolve(target).UploadedPhoto(ctx,file,captionOption...)}
		preserved:=make([]tg.DocumentAttributeClass,0,len(attrs));for _,attr:=range attrs{if _,isFilename:=attr.(*tg.DocumentAttributeFilename);!isFilename{preserved=append(preserved,attr)}}
		doc:=message.UploadedDocument(file,captionOption...).Filename(filename).MIME(mime).Attributes(preserved...)
		if peer!=nil{return s.To(peer).Media(ctx,doc)};return s.Resolve(target).Media(ctx,doc)
	})
}

type AlbumItem struct {
	Path, Filename, MIME, Kind, Caption string
	Attributes []tg.DocumentAttributeClass
	Entities []tg.MessageEntityClass
}

// UploadAlbumTo uploads a Telegram media group (2-10 photos/videos) and sends it
// as one grouped message. Other document types must use UploadPathTo individually.
func (c *Client) UploadAlbumTo(ctx context.Context,destination tg.InputPeerClass,items []AlbumItem)(tg.UpdatesClass,error){
	return c.UploadAlbumToWithProgress(ctx,destination,items,nil)
}
func (c *Client) UploadAlbumToWithProgress(ctx context.Context,destination tg.InputPeerClass,items []AlbumItem,progress TransferProgress)(tg.UpdatesClass,error){
	if len(items)<2||len(items)>10{return nil,fmt.Errorf("album must contain between 2 and 10 items")}
	if err:=c.Ready(ctx);err!=nil{return nil,err}
	c.mu.RLock();api,configuredPeer,target:=c.api,c.targetPeer,c.target;c.mu.RUnlock();peer:=destination;if peer==nil{peer=configuredPeer}
	if peer==nil&&strings.TrimSpace(target)==""{return nil,errors.New("album destination is required")}
	var total int64;for i,item:=range items{if item.Kind!="photo"&&item.Kind!="video"{return nil,fmt.Errorf("album item %d has unsupported kind %q; only photos and videos can be grouped",i+1,item.Kind)};info,err:=os.Stat(item.Path);if err!=nil{return nil,fmt.Errorf("stat album item %d: %w",i+1,err)};total+=info.Size()}
	return retryFloodWait(ctx,func()(tg.UpdatesClass,error){
		u:=newTransferUploader(api).WithProgress(&uploadProgress{total:total,callback:progress});s:=message.NewSender(api).WithUploader(u)
		options:=make([]message.MultiMediaOption,0,len(items))
		for i,item:=range items{
			file,err:=u.FromPath(ctx,item.Path);if err!=nil{return nil,fmt.Errorf("upload album item %d: %w",i+1,err)}
			caption:=captionOptions(item.Caption,item.Entities);var option message.MediaOption
			if item.Kind=="photo"{option=message.UploadedPhoto(file,caption...)}else{preserved:=make([]tg.DocumentAttributeClass,0,len(item.Attributes));for _,attr:=range item.Attributes{if _,isFilename:=attr.(*tg.DocumentAttributeFilename);!isFilename{preserved=append(preserved,attr)}};option=message.UploadedDocument(file,caption...).Filename(item.Filename).MIME(item.MIME).Attributes(preserved...)}
			options=append(options,message.ForceMulti(option))
		}
		if peer!=nil{return s.To(peer).Album(ctx,options[0],options[1:]...)}
		return s.Resolve(target).Album(ctx,options[0],options[1:]...)
	})
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
	return retryFloodWait(ctx,func()(tg.UpdatesClass,error){ if peer != nil { return s.To(peer).Text(ctx,text) }; if strings.TrimSpace(c.target)=="" { return nil,errors.New("text destination is required") }; return s.Resolve(c.target).Text(ctx,text) })
}

func (c *Client) ResolvePeer(ctx context.Context, target string) (tg.InputPeerClass, error) {
	if err := c.Ready(ctx); err != nil { return nil, err }
	c.mu.RLock(); api := c.api; c.mu.RUnlock()
	return resolveTarget(ctx, api, target)
}

func resolveTarget(ctx context.Context, api *tg.Client, target string) (tg.InputPeerClass, error) {
	target = strings.TrimSpace(target)
	if target == "" { return nil, errors.New("storage chat ID is empty") }
	id, err := strconv.ParseInt(target, 10, 64)
	if err != nil || id >= 0 { return nil, fmt.Errorf("invalid Telegram Bot API chat ID %q: expected a negative numeric ID", target) }

	if id > -1000000000000 {
		chatID := -id
		if chatID <= 0 || chatID > 2147483647 {
			return nil, fmt.Errorf("invalid basic group ID in chat ID %q", target)
		}
		return &tg.InputPeerChat{ChatID:chatID}, nil
	}

	channelID := -id - 1000000000000
	if channelID <= 0 || channelID > 997852516352 {
		return nil, fmt.Errorf("invalid Telegram channel ID in chat ID %q", target)
	}
	if api == nil { return nil, errors.New("MTProto API is required to resolve a channel destination") }

	// messages.getDialogs is users-only. channels.getChannels supports bots:
	// query with a zero access hash, then use the Channel constructor returned
	// by Telegram (not a fabricated InputPeerChannel) for subsequent RPCs.
	result, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{
		&tg.InputChannel{ChannelID:channelID, AccessHash:0},
	})
	if err != nil { return nil, fmt.Errorf("resolve channel %d with uploader bot: %w", channelID, err) }

	var chats []tg.ChatClass
	switch value := result.(type) {
	case *tg.MessagesChats: chats = value.Chats
	case *tg.MessagesChatsSlice: chats = value.Chats
	default: return nil, fmt.Errorf("unsupported channels.getChannels response %T", result)
	}
	peer, err := channelPeerFromChats(chats, channelID)
	if err != nil { return nil, fmt.Errorf("resolve channel %d with uploader bot: %w", channelID, err) }
	return peer, nil
}

func channelPeerFromChats(chats []tg.ChatClass, channelID int64) (tg.InputPeerClass, error) {
	for _, item := range chats {
		if channel, ok := item.(*tg.Channel); ok && channel.ID == channelID {
			return &tg.InputPeerChannel{ChannelID:channel.ID, AccessHash:channel.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("Telegram did not return channel %d; verify the uploader bot is a member", channelID)
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
