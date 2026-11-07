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
	"sync"
	"time"

	"github.com/aniketnotcoder/theogbot/internal/account"
	"github.com/aniketnotcoder/theogbot/internal/forward"
	"github.com/aniketnotcoder/theogbot/internal/links"
	"github.com/aniketnotcoder/theogbot/internal/media"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/aniketnotcoder/theogbot/internal/storage"
	"github.com/gotd/td/tg"
)

const telegramAPI = "https://api.telegram.org"

type Bot struct {
	token string
	username string
	client *http.Client
	log *slog.Logger
	offset int64
	accounts *account.Manager
	storage *storage.Client
	tempDir string
	jobsMu sync.Mutex
	jobs map[int64]context.CancelFunc
	destMu sync.RWMutex
	destinations map[int64]destination
	awaitingDestination map[int64]bool
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message *incomingMessage `json:"message"`
}
type forwardedChat struct { ID int64 `json:"id"`; Title string `json:"title"`; Username string `json:"username"` }
type forwardOrigin struct { Type string `json:"type"`; Chat *forwardedChat `json:"chat"`; SenderChat *forwardedChat `json:"sender_chat"` }
func (o *forwardOrigin) sourceChat() *forwardedChat { if o == nil { return nil }; if o.Chat != nil { return o.Chat }; return o.SenderChat }

type incomingMessage struct {
	MessageID int64 `json:"message_id"`
	Text string `json:"text"`
	Chat struct { ID int64 `json:"id"`; Type string `json:"type"` } `json:"chat"`
	From struct { ID int64 `json:"id"` } `json:"from"`
	ForwardOrigin *forwardOrigin `json:"forward_origin"`
}
type destination struct { ChatID int64; Title string; Username string }
type botIdentity struct { Username string `json:"username"` }
type sentMessage struct { MessageID int64 `json:"message_id"` }

type progressReporter struct { bot *Bot; chatID, messageID int64; label string; total int; started time.Time; mu sync.Mutex; last time.Time; bytesCurrent,bytesTotal int64; speed float64; sampleAt time.Time; sampleBytes int64; sampleStage string; sampleTotal int64 }

func (b *Bot) newProgress(ctx context.Context, chatID int64, label string, total int) (*progressReporter, error) {
	var sent sentMessage
	if err := b.call(ctx, "sendMessage", url.Values{"chat_id":{strconv.FormatInt(chatID,10)}, "text":{fmt.Sprintf("%s: preparing %d messages…", label,total)}}, &sent); err != nil { return nil, err }
	return &progressReporter{bot:b,chatID:chatID,messageID:sent.MessageID,label:label,total:total,started:time.Now()},nil
}

func formatProgress(label, stage string, current, total, done, failed, skipped int, elapsed time.Duration) string {
	processed := done+failed+skipped
	percent := 0
	if total > 0 { percent = processed*100/total }
	eta := "calculating…"
	if processed > 0 && total > processed { remaining := time.Duration(float64(elapsed)*float64(total-processed)/float64(processed)); eta = remaining.Round(time.Second).String() } else if processed >= total { eta = "0s" }
	return fmt.Sprintf("%s · %d%%\nStage: %s\nItem: %d/%d\nUploaded: %d · Failed: %d · Skipped: %d\nElapsed: %s · ETA: %s",label,percent,stage,current,total,done,failed,skipped,elapsed.Round(time.Second),eta)
}

func humanBytes(n int64) string { const unit=1024; if n<unit{return fmt.Sprintf("%d B",n)}; div,exp:=int64(unit),0;for v:=n/unit;v>=unit;v/=unit{div*=unit;exp++};return fmt.Sprintf("%.1f %ciB",float64(n)/float64(div),"KMGTPE"[exp]) }
func humanSpeed(n float64) string { if n<=0{return "calculating…"};return humanBytes(int64(n))+"/s" }
func (p *progressReporter) recordByteSample(stage string,current,total int64,now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sampleAt.IsZero() || p.sampleStage != stage || p.sampleTotal != total || current < p.sampleBytes {
		p.speed = 0
		p.sampleBytes, p.sampleAt = current, now
		p.sampleStage, p.sampleTotal = stage, total
		return
	}
	elapsed := now.Sub(p.sampleAt)
	if elapsed < time.Second { return }
	instant := float64(current-p.sampleBytes) / elapsed.Seconds()
	if p.speed == 0 { p.speed = instant } else { p.speed = p.speed*0.65 + instant*0.35 }
	p.sampleBytes, p.sampleAt = current, now
}

func (p *progressReporter) updateBytes(ctx context.Context,stage string,current,total int64,item,done,failed,skipped int){
	if p==nil{return}
	now:=time.Now()
	p.recordByteSample(stage,current,total,now)
	p.mu.Lock()
	p.bytesCurrent,p.bytesTotal=current,total
	p.mu.Unlock()
	p.update(ctx,stage,item,done,failed,skipped,false)
}
func (p *progressReporter) update(ctx context.Context, stage string, current, done, failed, skipped int, force bool) {
	if p==nil||p.messageID==0{return};now:=time.Now();p.mu.Lock()
	if !force&&now.Sub(p.last)<900*time.Millisecond{p.mu.Unlock();return};p.last=now
	bytesCurrent,bytesTotal,speed:=p.bytesCurrent,p.bytesTotal,p.speed;p.mu.Unlock()
	text:=formatProgress(p.label,stage,current,p.total,done,failed,skipped,now.Sub(p.started))
	if bytesCurrent>0{if bytesTotal>0{percent:=int(float64(bytesCurrent)*100/float64(bytesTotal));if percent>100{percent=100};text+=fmt.Sprintf("\nBytes: %s / %s (%d%%)",humanBytes(bytesCurrent),humanBytes(bytesTotal),percent)}else{text+="\nBytes: "+humanBytes(bytesCurrent)};text+="\nSpeed: "+humanSpeed(speed);if bytesTotal>bytesCurrent&&speed>0{text+=" · Byte ETA: "+(time.Duration(float64(bytesTotal-bytesCurrent)/speed*float64(time.Second))).Round(time.Second).String()}}
	values:=url.Values{"chat_id":{strconv.FormatInt(p.chatID,10)},"message_id":{strconv.FormatInt(p.messageID,10)},"text":{text}}
	if err:=p.bot.call(ctx,"editMessageText",values,nil);err!=nil{p.bot.log.Warn("progress message update failed","chat_id",p.chatID,"error",err)}
}
type apiResponse[T any] struct {
	OK bool `json:"ok"`
	Description string `json:"description"`
	Result T `json:"result"`
}

func New(token string, logger *slog.Logger, accounts *account.Manager) *Bot {
	return &Bot{token: token, client: &http.Client{
		Timeout: 45*time.Second,
		Transport: &http.Transport{Proxy:http.ProxyFromEnvironment,MaxIdleConns:100,MaxIdleConnsPerHost:20,IdleConnTimeout:90*time.Second,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:40*time.Second,DialContext:(&net.Dialer{Timeout:10*time.Second,KeepAlive:30*time.Second}).DialContext},
	},log:logger,accounts:accounts,jobs:make(map[int64]context.CancelFunc),destinations:make(map[int64]destination),awaitingDestination:make(map[int64]bool)}
}

func (b *Bot) SetStorage(client *storage.Client) { b.storage = client }
func (b *Bot) SetTempDir(path string) { b.tempDir = path }

func (b *Bot) Run(ctx context.Context) error {
	var me botIdentity
	if err:=b.call(ctx,"getMe",nil,&me);err!=nil{return fmt.Errorf("telegram getMe: %w",err)}
	b.username=me.Username
	if b.username=="" { return fmt.Errorf("Telegram did not return the bot username") }
	b.log.Info("bot connected; long polling started","username",b.username)
	for {
		if ctx.Err()!=nil{return nil}
		var updates []update
		err:=b.call(ctx,"getUpdates",url.Values{"offset":{strconv.FormatInt(b.offset,10)},"timeout":{"30"},"allowed_updates":{"[\"message\"]"}},&updates)
		if err!=nil { if ctx.Err()!=nil{return nil};b.log.Warn("getUpdates failed","error",err);select{case <-ctx.Done():return nil;case <-time.After(2*time.Second):};continue }
		for _,u:=range updates { if u.UpdateID>=b.offset{b.offset=u.UpdateID+1};if u.Message!=nil{b.handleMessage(ctx,u.Message)} }
	}
}

func (b *Bot) handleMessage(ctx context.Context,msg *incomingMessage) {
	text:=strings.TrimSpace(msg.Text);if msg.Chat.ID==0{return};if b.captureForwardedDestination(ctx,msg) { return };if text=="" { return }
	fields:=strings.Fields(text);command:=strings.ToLower(fields[0]);if i:=strings.IndexByte(command,'@');i>=0{command=command[:i]}
	var reply string
	if (command=="/code"||command=="/password")&&msg.Chat.Type!="private"{_=b.deleteMessage(ctx,msg.Chat.ID,msg.MessageID);_=b.sendMessage(ctx,msg.Chat.ID,"For account security, send login codes and 2FA passwords only in a private chat with this bot.");return}
	switch command {
	case "/start":
		reply="Welcome to TheOGBot.\n\n/login +COUNTRYCODEPHONE — connect your Telegram account\n/code CODE — submit login code\n/password PASSWORD — submit 2FA password\n/batch LINK [LINK ...] — transfer selected posts\n/copy CHAT_LINK [LIMIT] — copy accessible chat history (default: 100, max: 50000)\n/cancel — cancel your active transfer\n/setstorage — configure destination by forwarding a message\n/storage — your destination status\n/logout — disconnect\n/info — account status"
	case "/help":
		reply="Commands:\n/start — welcome\n/login +COUNTRYCODEPHONE — begin MTProto login\n/code CODE — submit login code\n/password PASSWORD — submit 2FA password\n/batch LINK [LINK ...] — transfer only the linked posts, in supplied order\n/copy CHAT_LINK [LIMIT] — copy accessible history, default 100, max 50000\n/cancel — cancel your active transfer\n/setstorage — configure destination by forwarding a message\n/storage — destination status\n/logout — disconnect\n/info — account status\n\nUse chats your account can access. Telegram restrictions and flood waits are respected."
	case "/login":
		if len(fields)<2{reply="Usage: /login +COUNTRYCODEPHONE\nThen /code YOUR_CODE and, if prompted, /password YOUR_2FA_PASSWORD"}else if b.accounts==nil{reply="MTProto unavailable. Configure API_ID and API_HASH."}else if b.accounts.Connected(msg.From.ID){reply="An account session is already active. Use /logout first."}else if err:=b.accounts.Begin(ctx,msg.From.ID,fields[1]);err!=nil{reply="Could not start login: "+err.Error()}else{reply="Login started. Submit the Telegram code here using /code YOUR_CODE."}
	case "/code":
		_=b.deleteMessage(ctx,msg.Chat.ID,msg.MessageID)
		if len(fields)!=2{reply="Usage: /code YOUR_TELEGRAM_CODE"}else if err:=b.accounts.SubmitCode(msg.From.ID,fields[1]);err!=nil{reply="Could not submit code: "+err.Error()}else{reply="Code submitted. If Telegram requests 2-step verification, send /password YOUR_PASSWORD."}
	case "/password":
		_=b.deleteMessage(ctx,msg.Chat.ID,msg.MessageID);password:=strings.TrimSpace(strings.TrimPrefix(text,fields[0]))
		if password==""{reply="Usage: /password YOUR_2FA_PASSWORD"}else if err:=b.accounts.SubmitPassword(msg.From.ID,password);err!=nil{reply="Could not submit password: "+err.Error()}else{reply="2-step verification password submitted."}
	case "/batch":
		if len(fields)<2{reply="Usage: /batch https://t.me/channel/123 [https://t.me/channel/124]"}else if jobCtx,ok:=b.startJob(msg.From.ID,ctx);!ok{reply="A transfer is already running. Send /cancel first."}else{go func(){defer b.finishJob(msg.From.ID);b.runBatch(jobCtx,msg.From.ID,msg.Chat.ID,fields[1:])}();return}
	case "/copy":
		if len(fields)<2{reply="Usage: /copy https://t.me/channel [LIMIT]"}else{limit:=100;if len(fields)>2{n,err:=strconv.Atoi(fields[2]);if err!=nil||n<1||n>50000{reply="LIMIT must be between 1 and 50000."}else{limit=n}};if reply==""{if jobCtx,ok:=b.startJob(msg.From.ID,ctx);!ok{reply="A transfer is already running. Send /cancel first."}else{go func(){defer b.finishJob(msg.From.ID);b.runCopy(jobCtx,msg.From.ID,msg.Chat.ID,fields[1],limit)}();return}}}
	case "/cancel":
		if b.cancelJob(msg.From.ID){reply="Cancellation requested. The current operation will stop safely."}else{reply="No active transfer to cancel."}
	case "/setstorage":
		if msg.Chat.Type != "private" { reply = "Configure your destination in a private chat with this bot." } else {
			b.destMu.Lock()
			b.awaitingDestination[msg.From.ID] = true
			b.destMu.Unlock()
			reply = "Forward any message from your destination group/channel to this bot. The uploader bot must already be a member with posting permission."
		}
	case "/storage":
		if b.storage == nil { reply = "Storage uploader is not configured." } else if err := b.storage.Ready(ctx); err != nil { reply = "Storage uploader is not ready: " + err.Error() } else if d, ok := b.getDestination(msg.From.ID); ok { reply = fmt.Sprintf("Uploader: connected\nYour destination: %s\nChat ID: %d\nAccess: visible to uploader bot; posting rights are checked on transfer", d.Title, d.ChatID) } else { reply = "Uploader: connected\nNo personal destination configured. Send /setstorage, then forward a message from your destination group/channel." }
	case "/logout":
		if b.accounts!=nil&&b.accounts.Logout(msg.From.ID){reply="Your in-memory Telegram session has been disconnected."}else{reply="No active Telegram session."}
	case "/info":
		status:="disconnected";if b.accounts!=nil{status=b.accounts.Status(msg.From.ID)}
		reply=fmt.Sprintf("TheOGBot\nBackend: Go\nMTProto: %s\nSession storage: memory only\nTransfer: MTProto download → upload",status)
	default: reply="Unknown command. Send /help."
	}
	if err:=b.sendMessage(ctx,msg.Chat.ID,reply);err!=nil{b.log.Error("send message failed","chat_id",msg.Chat.ID,"error",err)}
}

func (b *Bot) runBatch(parent context.Context,userID,chatID int64,rawLinks []string) {
	ctx,cancel:=context.WithTimeout(parent,30*time.Minute);defer cancel()
	api,ok:=b.accounts.API(userID);if !ok{_=b.sendMessage(ctx,chatID,"Connect your Telegram account first with /login.");return}
	var source tg.InputPeerClass;ids:=make([]int,0,len(rawLinks))
	for _,raw:=range rawLinks{
		parsed,err:=links.Parse(raw);if err!=nil{_=b.sendMessage(ctx,chatID,"Invalid Telegram post link: "+raw);return}
		var peer tg.InputPeerClass
		if parsed.Username!=""{peer,err=forward.ResolveUsername(ctx,api,parsed.Username)}else{peer,err=forward.ResolvePrivateChannelID(ctx,api,parsed.ChannelID)}
		if err!=nil{_=b.sendMessage(ctx,chatID,"Cannot access source chat: "+err.Error());return}
		if source!=nil&&peerKey(source)!=peerKey(peer){_=b.sendMessage(ctx,chatID,"All /batch links must belong to the same source chat.");return}
		source=peer;ids=append(ids,parsed.MessageID)
	}
	b.executeTransfer(ctx,userID,api,source,chatID,ids,"Batch")
}

func (b *Bot) runCopy(parent context.Context,userID,chatID int64,raw string,limit int) {
	ctx,cancel:=context.WithTimeout(parent,30*time.Minute);defer cancel()
	api,ok:=b.accounts.API(userID);if !ok{_=b.sendMessage(ctx,chatID,"Connect your Telegram account first with /login.");return}
	username,channelID,err:=parseChatLink(raw);if err!=nil{_=b.sendMessage(ctx,chatID,err.Error());return}
	var source tg.InputPeerClass
	if username!=""{source,err=forward.ResolveUsername(ctx,api,username)}else{source,err=forward.ResolvePrivateChannelID(ctx,api,channelID)}
	if err!=nil{_=b.sendMessage(ctx,chatID,"Cannot access source chat: "+err.Error());return}
	_=b.sendMessage(ctx,chatID,fmt.Sprintf("Reading accessible history (limit %d)…",limit))
	ids,err:=forward.FetchHistoryIDs(ctx,api,source,limit);if err!=nil{_=b.sendMessage(ctx,chatID,"History retrieval failed: "+err.Error());return}
	if len(ids)==0{_=b.sendMessage(ctx,chatID,"No messages found in accessible history.");return}
	b.executeTransfer(ctx,userID,api,source,chatID,ids,"Copy")
}

func (b *Bot) executeTransfer(ctx context.Context, userID int64, api *tg.Client, source tg.InputPeerClass, chatID int64, ids []int, label string) {
	if b.storage == nil {
		_ = b.sendMessage(ctx, chatID, "Storage uploader is not configured.")
		return
	}
	if err := b.storage.Ready(ctx); err != nil {
		_ = b.sendMessage(ctx, chatID, "Storage uploader is not ready: "+err.Error())
		return
	}
	d, ok := b.getDestination(userID)
	if !ok { _ = b.sendMessage(ctx, chatID, "Set your destination first: send /setstorage, then forward a message from your destination group/channel."); return }
	destinationPeer, err := b.storage.ResolvePeer(ctx, strconv.FormatInt(d.ChatID,10))
	if err != nil { _ = b.sendMessage(ctx, chatID, "Your destination is not accessible to the uploader bot: "+err.Error()+". Add the configured uploader bot with posting permission, then run /setstorage again."); return }
	if len(ids) == 0 { _ = b.sendMessage(ctx, chatID, label+": no messages to transfer."); return }
	progress, progressErr := b.newProgress(ctx, chatID, label, len(ids))
	resultRefs := make([]string, 0, len(ids))
	if progressErr != nil { _ = b.sendMessage(ctx,chatID,fmt.Sprintf("%s started: %d messages, but progress reporting could not start: %v",label,len(ids),progressErr)) } else { progress.update(ctx,"Retrieving source messages",1,0,0,0,true) }
	elements, err := forward.FetchMessagesByID(ctx, api, source, ids)
	if err != nil { if progress != nil { progress.update(ctx,"Failed to retrieve source messages",1,0,1,0,true) }; _ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s could not retrieve the exact requested posts: %v", label, err)); return }

	done, failed, skipped := 0, 0, 0
	if progress != nil { progress.update(ctx,"Ready",1,done,failed,skipped,true) }
	for i := 0; i < len(ids); {
		if err := ctx.Err(); err != nil {
			if progress != nil { progress.update(context.Background(),"Cancelled",i+1,done,failed,skipped,true) }
			_ = b.sendMessage(context.Background(), chatID, fmt.Sprintf("%s cancelled after %d/%d (failed %d, skipped %d).", label, done, len(ids), failed, skipped))
			b.sendTransferResults(context.Background(),chatID,d,label,done,resultRefs)
			return
		}

		// Telegram albums arrive as adjacent messages sharing grouped_id. Keep
		// the caller's order and only combine consecutive selected album members.
		groupLen := albumGroupLength(elements, i)
		if progress != nil { progress.update(ctx,"Downloading album",i+1,done,failed,skipped,false) }
		if groupLen > 1 {
			downloads := make([]*media.Downloaded, 0, groupLen)
			albumReady := true
			for j := 0; j < groupLen; j++ {
				item, downloadErr := media.DownloadMessageWithProgress(ctx, api, elements[i+j], b.tempDir, func(current,total int64){if progress!=nil{progress.updateBytes(ctx,fmt.Sprintf("Downloading album %d/%d",j+1,groupLen),current,total,i+j+1,done,failed,skipped)}})
				if downloadErr != nil { albumReady = false; break }
				if item.Kind != "photo" && item.Kind != "video" { _ = item.Cleanup(); albumReady = false; break }
				downloads = append(downloads, item)
			}
			if albumReady && len(downloads) == groupLen {
				items := make([]storage.AlbumItem, 0, groupLen)
				for _, item := range downloads {
					items = append(items, storage.AlbumItem{Path:item.Path, Filename:item.Name, MIME:item.MIME, Kind:item.Kind, Caption:item.Caption, Attributes:item.Attributes, Entities:item.Entities})
				}
				if progress != nil { progress.update(ctx,"Uploading album",i+1,done,failed,skipped,true) }
				updates, uploadErr := b.storage.UploadAlbumToWithProgress(ctx, destinationPeer, items, func(current,total int64){if progress!=nil{progress.updateBytes(ctx,"Uploading album",current,total,i+1,done,failed,skipped)}})
				for _, item := range downloads { if cleanupErr := item.Cleanup(); cleanupErr != nil { b.log.Warn("temporary album file cleanup failed", "path", item.Path, "error", cleanupErr) } }
				if uploadErr != nil {
					failed += groupLen
					_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s album [%d-%d/%d] upload failed: %v", label, i+1, i+groupLen, len(ids), uploadErr))
				} else {
					done += groupLen
					resultRefs = append(resultRefs, destinationMessageRefs(d, updates)...)
				}
				if progress != nil { progress.update(ctx,"Transferring",i,done,failed,skipped,false) }
				i += groupLen
				continue
			}
			for _, item := range downloads { _ = item.Cleanup() }
			// If the source album contains an unsupported item or one member could
			// not be downloaded, retry its messages individually below.
		}

		elem := elements[i]
		id := ids[i]
		if progress != nil { progress.update(ctx,"Downloading",i+1,done,failed,skipped,false) }
		downloaded, downloadErr := media.DownloadMessageWithProgress(ctx, api, elem, b.tempDir, func(current,total int64){if progress!=nil{progress.updateBytes(ctx,"Downloading",current,total,i+1,done,failed,skipped)}})
		if downloadErr != nil {
			if msg, ok := elem.Msg.(*tg.Message); ok && strings.TrimSpace(msg.Message) != "" && msg.Media == nil {
				if updates, textErr := b.storage.SendTextTo(ctx, destinationPeer, msg.Message); textErr != nil {
					failed++
					_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s [%d/%d] text upload failed for message %d: %v", label, i+1, len(ids), id, textErr))
				} else { done++; resultRefs = append(resultRefs, destinationMessageRefs(d, updates)...)}
			} else {
				skipped++
				_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s [%d/%d] skipped message %d: %v", label, i+1, len(ids), id, downloadErr))
			}
			i++
			continue
		}
		if progress != nil { progress.update(ctx,"Uploading",i+1,done,failed,skipped,false) }
		updates, uploadErr := b.storage.UploadPathToWithProgress(ctx, destinationPeer, downloaded.Path, downloaded.Name, downloaded.MIME, downloaded.Caption, downloaded.Kind, downloaded.Attributes, downloaded.Entities, func(current,total int64){if progress!=nil{progress.updateBytes(ctx,"Uploading",current,total,i+1,done,failed,skipped)}})
		cleanupErr := downloaded.Cleanup()
		if uploadErr != nil {
			failed++
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s [%d/%d] upload failed for message %d: %v", label, i+1, len(ids), id, uploadErr))
		} else {
			if cleanupErr != nil { b.log.Warn("temporary file cleanup failed", "path", downloaded.Path, "error", cleanupErr) }
			done++
			resultRefs = append(resultRefs, destinationMessageRefs(d, updates)...)
		}
		i++
	}
	if progress != nil { progress.update(ctx,"Finished",len(ids),done,failed,skipped,true) }
	_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s finished. Uploaded: %d, failed: %d, skipped: %d, requested: %d.", label, done, failed, skipped, len(ids)))
	b.sendTransferResults(ctx,chatID,d,label,done,resultRefs)
}

func (b *Bot) sendTransferResults(ctx context.Context, chatID int64, dest destination, label string, done int, refs []string) {
	if len(refs) > 0 {
		for start := 0; start < len(refs); start += 15 {
			end := start+15
			if end > len(refs) { end = len(refs) }
			_ = b.sendMessage(ctx,chatID,fmt.Sprintf("%s destination messages (%d-%d):\n%s",label,start+1,end,strings.Join(refs[start:end],"\n")))
		}
	} else if done > 0 {
		_ = b.sendMessage(ctx,chatID,fmt.Sprintf("Uploaded %d messages to destination chat %d, but Telegram did not return message IDs for link generation.",done,dest.ChatID))
	}
}

func destinationMessageRefs(dest destination, updates tg.UpdatesClass) []string {
	ids := make([]int, 0, 10)
	seen := make(map[int]struct{})
	add := func(id int) { if id > 0 { if _, ok := seen[id]; !ok { seen[id] = struct{}{}; ids = append(ids,id) } } }
	var collectUpdate func(tg.UpdateClass)
	collectUpdate = func(update tg.UpdateClass) {
		switch item := update.(type) {
		case *tg.UpdateNewMessage:
			if item.Message != nil { add(item.Message.GetID()) }
		case *tg.UpdateNewChannelMessage:
			if item.Message != nil { add(item.Message.GetID()) }
		}
	}
	switch result := updates.(type) {
	case *tg.Updates:
		for _, item := range result.Updates { collectUpdate(item) }
	case *tg.UpdatesCombined:
		for _, item := range result.Updates { collectUpdate(item) }
	case *tg.UpdateShort:
		collectUpdate(result.Update)
	case *tg.UpdateShortSentMessage:
		add(result.ID)
	}
	refs := make([]string,0,len(ids))
	username := strings.TrimPrefix(strings.TrimSpace(dest.Username),"@")
	channelID := int64(0)
	if dest.ChatID <= -1000000000000 { channelID = -dest.ChatID-1000000000000 }
	for _, id := range ids {
		switch {
		case username != "":
			refs = append(refs,fmt.Sprintf("https://t.me/%s/%d",username,id))
		case channelID > 0:
			refs = append(refs,fmt.Sprintf("https://t.me/c/%d/%d",channelID,id))
		default:
			refs = append(refs,fmt.Sprintf("chat %d · message %d",dest.ChatID,id))
		}
	}
	return refs
}

func albumGroupLength(elements []messages.Elem, start int) int {
	if start < 0 || start >= len(elements) { return 1 }
	first, ok := elements[start].Msg.(*tg.Message)
	if !ok || first.GroupedID == 0 { return 1 }
	count := 1
	for i := start+1; i < len(elements) && count < 10; i++ {
		msg, ok := elements[i].Msg.(*tg.Message)
		if !ok || msg.GroupedID != first.GroupedID { break }
		count++
	}
	return count
}

func parseChatLink(raw string)(string,int64,error){
	u,err:=url.Parse(strings.TrimSpace(raw));if err!=nil{return "",0,fmt.Errorf("Invalid chat link. Use https://t.me/username or https://t.me/c/CHANNEL_ID")}
	host:=strings.ToLower(u.Hostname());if host!="t.me"&&host!="telegram.me"&&host!="www.t.me"&&host!="www.telegram.me"{return "",0,fmt.Errorf("Unsupported Telegram link host.")}
	parts:=strings.Split(strings.Trim(u.Path,"/"),"/")
	// /copy accepts either a chat link or a message permalink. Ignore the
	// trailing message ID because /copy operates on the whole chat history.
	if len(parts)==2 && validChatUsername(parts[0]) {
		if _, e := strconv.Atoi(parts[1]); e == nil { parts = parts[:1] }
	}
	if len(parts)==3 && parts[0]=="c" {
		if _, e := strconv.Atoi(parts[2]); e == nil { parts = parts[:2] }
	}
	if len(parts)==1&&validChatUsername(parts[0]){return parts[0],0,nil}
	if len(parts)==2&&parts[0]=="c"{id,e:=strconv.ParseInt(parts[1],10,64);if e==nil&&id>0{return "",id,nil}}
	return "",0,fmt.Errorf("Use a chat link like https://t.me/username, https://t.me/username/123, or https://t.me/c/CHANNEL_ID/123")
}
func validChatUsername(s string)bool{return len(s)>=5&&len(s)<=32&&!strings.HasPrefix(s,"+")}
func peerKey(peer tg.InputPeerClass) string {
	switch p:=peer.(type) {
	case *tg.InputPeerChannel: return fmt.Sprintf("channel:%d",p.ChannelID)
	case *tg.InputPeerChat: return fmt.Sprintf("chat:%d",p.ChatID)
	case *tg.InputPeerUser: return fmt.Sprintf("user:%d",p.UserID)
	default: return fmt.Sprintf("%T",peer)
	}
}


func (b *Bot) getDestination(userID int64) (destination, bool) {
	b.destMu.RLock()
	defer b.destMu.RUnlock()
	d, ok := b.destinations[userID]
	return d, ok
}

func (b *Bot) captureForwardedDestination(ctx context.Context, msg *incomingMessage) bool {
	b.destMu.Lock()
	pending := b.awaitingDestination[msg.From.ID]
	if pending && msg.ForwardOrigin != nil && (msg.ForwardOrigin.Chat != nil || msg.ForwardOrigin.SenderChat != nil) { delete(b.awaitingDestination, msg.From.ID) }
	b.destMu.Unlock()
	if !pending || msg.ForwardOrigin == nil || (msg.ForwardOrigin.Chat == nil && msg.ForwardOrigin.SenderChat == nil) { return false }
	if msg.Chat.Type != "private" { _ = b.sendMessage(ctx,msg.Chat.ID,"Please forward the destination message in a private chat with this bot."); return true }
	origin := msg.ForwardOrigin
	forwardedChat := origin.Chat
	if forwardedChat == nil { forwardedChat = origin.SenderChat }
	if origin.Type != "chat" && origin.Type != "channel" { _ = b.sendMessage(ctx,msg.Chat.ID,"Please forward a message from a group or channel, not a user or hidden source."); return true }
	if forwardedChat == nil || forwardedChat.ID >= 0 { _ = b.sendMessage(ctx,msg.Chat.ID,"That forwarded message does not identify a group/channel destination."); return true }
	if b.storage == nil { _ = b.sendMessage(ctx,msg.Chat.ID,"Uploader is not configured."); return true }
	resolveCtx,cancel := context.WithTimeout(ctx,15*time.Second)
	defer cancel()
	if _,err := b.storage.ResolvePeer(resolveCtx,strconv.FormatInt(forwardedChat.ID,10)); err != nil {
		_ = b.sendMessage(ctx,msg.Chat.ID,"The uploader bot cannot access this destination: "+err.Error()+". Add it to the destination with posting permission, then run /setstorage again.")
		return true
	}
	title := strings.TrimSpace(forwardedChat.Title)
	if title=="" { title=forwardedChat.Username }
	if title=="" { title=strconv.FormatInt(forwardedChat.ID,10) }
	b.destMu.Lock()
	b.destinations[msg.From.ID]=destination{ChatID:forwardedChat.ID,Title:title,Username:forwardedChat.Username}
	b.destMu.Unlock()
	_ = b.sendMessage(ctx,msg.Chat.ID,fmt.Sprintf("Destination saved for your account.\nName: %s\nChat ID: %d\nUploader bot can see this chat. Posting permission will be checked on transfer.",title,forwardedChat.ID))
	return true
}

func (b *Bot) deleteMessage(ctx context.Context,chatID,messageID int64)error{return b.call(ctx,"deleteMessage",url.Values{"chat_id":{strconv.FormatInt(chatID,10)},"message_id":{strconv.FormatInt(messageID,10)}},nil)}
func (b *Bot) sendMessage(ctx context.Context,chatID int64,text string)error{return b.call(ctx,"sendMessage",url.Values{"chat_id":{strconv.FormatInt(chatID,10)},"text":{text}},nil)}
func (b *Bot) call(ctx context.Context,method string,values url.Values,out any)error{
	if values==nil{values=url.Values{}}
	req,err:=http.NewRequestWithContext(ctx,http.MethodPost,telegramAPI+"/bot"+b.token+"/"+method,strings.NewReader(values.Encode()));if err!=nil{return err}
	req.Header.Set("Content-Type","application/x-www-form-urlencoded")
	resp,err:=b.client.Do(req);if err!=nil{return err};defer resp.Body.Close()
	var result apiResponse[json.RawMessage];if err:=json.NewDecoder(resp.Body).Decode(&result);err!=nil{return err}
	if resp.StatusCode<200||resp.StatusCode>=300||!result.OK{return fmt.Errorf("telegram API %s: %s (HTTP %d)",method,result.Description,resp.StatusCode)}
	if out!=nil&&len(result.Result)>0{return json.Unmarshal(result.Result,out)}
	return nil
}

func (b *Bot) startJob(userID int64, parent context.Context) (context.Context, bool) {
	b.jobsMu.Lock()
	defer b.jobsMu.Unlock()
	if _, exists := b.jobs[userID]; exists { return nil, false }
	ctx, cancel := context.WithCancel(parent)
	b.jobs[userID] = cancel
	return ctx, true
}

func (b *Bot) finishJob(userID int64) {
	b.jobsMu.Lock()
	if cancel := b.jobs[userID]; cancel != nil { cancel(); delete(b.jobs, userID) }
	b.jobsMu.Unlock()
}

func (b *Bot) cancelJob(userID int64) bool {
	b.jobsMu.Lock()
	cancel := b.jobs[userID]
	b.jobsMu.Unlock()
	if cancel == nil { return false }
	cancel()
	return true
}
