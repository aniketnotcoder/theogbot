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
type botIdentity struct { Username string `json:"username"` }
type apiResponse[T any] struct {
	OK bool `json:"ok"`
	Description string `json:"description"`
	Result T `json:"result"`
}

func New(token string, logger *slog.Logger, accounts *account.Manager) *Bot {
	return &Bot{token: token, client: &http.Client{
		Timeout: 45*time.Second,
		Transport: &http.Transport{Proxy:http.ProxyFromEnvironment,MaxIdleConns:100,MaxIdleConnsPerHost:20,IdleConnTimeout:90*time.Second,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:40*time.Second,DialContext:(&net.Dialer{Timeout:10*time.Second,KeepAlive:30*time.Second}).DialContext},
	},log:logger,accounts:accounts,jobs:make(map[int64]context.CancelFunc)}
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
	text:=strings.TrimSpace(msg.Text);if text==""||msg.Chat.ID==0{return}
	fields:=strings.Fields(text);command:=strings.ToLower(fields[0]);if i:=strings.IndexByte(command,'@');i>=0{command=command[:i]}
	var reply string
	if (command=="/code"||command=="/password")&&msg.Chat.Type!="private"{_=b.deleteMessage(ctx,msg.Chat.ID,msg.MessageID);_=b.sendMessage(ctx,msg.Chat.ID,"For account security, send login codes and 2FA passwords only in a private chat with this bot.");return}
	switch command {
	case "/start":
		reply="Welcome to TheOGBot.\n\n/login +COUNTRYCODEPHONE — connect your Telegram account\n/code CODE — submit login code\n/password PASSWORD — submit 2FA password\n/batch LINK [LINK ...] — download and upload selected posts\n/copy CHAT_LINK [LIMIT] — download and upload accessible chat history\n/logout — disconnect\n/info — account status\\n/storage — storage uploader status"
	case "/help":
		reply="Commands:\n/start — welcome\n/login +COUNTRYCODEPHONE — begin MTProto login\n/code CODE — submit login code\n/password PASSWORD — submit 2FA password\n/batch LINK [LINK ...] — download and upload selected media\n/copy CHAT_LINK [LIMIT] — download and upload history, default 50000, max 50000\n/logout — disconnect\n/info — account status\n\nUse only chats your account can access. Telegram restrictions and flood waits are respected."
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
		if len(fields)<2{reply="Usage: /copy https://t.me/channel [LIMIT]"}else{limit:=50000;if len(fields)>2{n,err:=strconv.Atoi(fields[2]);if err!=nil||n<1||n>50000{reply="LIMIT must be between 1 and 50000."}else{limit=n}};if reply==""{if jobCtx,ok:=b.startJob(msg.From.ID,ctx);!ok{reply="A transfer is already running. Send /cancel first."}else{go func(){defer b.finishJob(msg.From.ID);b.runCopy(jobCtx,msg.From.ID,msg.Chat.ID,fields[1],limit)}();return}}}
	case "/cancel":
		if b.cancelJob(msg.From.ID){reply="Cancellation requested. The current operation will stop safely."}else{reply="No active transfer to cancel."}
	case "/storage":
		if b.storage == nil { reply = "Storage uploader is not configured. Set STORAGE_TARGET and restart." } else if err := b.storage.Ready(ctx); err != nil { reply = "Storage uploader is not ready: " + err.Error() } else { reply = "Storage uploader: connected\\nDestination: configured\\nUpload mode: download → upload" }
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
	b.executeTransfer(ctx,api,source,chatID,ids,"Batch")
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
	b.executeTransfer(ctx,api,source,chatID,ids,"Copy")
}

func (b *Bot) executeTransfer(ctx context.Context, api *tg.Client, source tg.InputPeerClass, chatID int64, ids []int, label string) {
	if b.storage == nil {
		_ = b.sendMessage(ctx, chatID, "Storage uploader is not configured. Set STORAGE_TARGET and restart.")
		return
	}
	if err := b.storage.Ready(ctx); err != nil {
		_ = b.sendMessage(ctx, chatID, "Storage uploader is not ready: "+err.Error())
		return
	}
	if len(ids) == 0 {
		_ = b.sendMessage(ctx, chatID, label+": no messages to transfer.")
		return
	}
	_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s started: %d messages. Mode: download → upload.", label, len(ids)))
	wanted := make(map[int]bool, len(ids))
	for _, id := range ids { wanted[id] = true }
	found := make(map[int]messages.Elem, len(ids))
	iter := messages.NewQueryBuilder(api).GetHistory(source).BatchSize(100).Iter()
	for iter.Next(ctx) {
		elem := iter.Value()
		id := elem.Msg.GetID()
		if wanted[id] { found[id] = elem; delete(wanted,id) }
		if len(wanted)==0 { break }
	}
	if err := iter.Err(); err != nil {
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s could not read source messages: %v", label, err))
		return
	}
	if len(wanted)>0 {
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s stopped: %d of %d requested messages could not be retrieved from source history.",label,len(wanted),len(ids)))
		return
	}
	done, failed, skipped := 0,0,0
	for i,id := range ids {
		if err := ctx.Err(); err != nil {
			_ = b.sendMessage(context.Background(),chatID,fmt.Sprintf("%s cancelled after %d/%d (failed %d, skipped %d).",label,done,len(ids),failed,skipped))
			return
		}
		elem := found[id]
		downloaded, err := media.DownloadMessage(ctx,api,elem,b.tempDir)
		if err != nil {
			skipped++
			_ = b.sendMessage(ctx,chatID,fmt.Sprintf("%s [%d/%d] skipped message %d: %v",label,i+1,len(ids),id,err))
			continue
		}
		_, uploadErr := b.storage.UploadPath(ctx,downloaded.Path,downloaded.Name,downloaded.MIME,"")
		cleanupErr := downloaded.Cleanup()
		if uploadErr != nil {
			failed++
			_ = b.sendMessage(ctx,chatID,fmt.Sprintf("%s [%d/%d] upload failed for message %d: %v",label,i+1,len(ids),id,uploadErr))
			continue
		}
		if cleanupErr != nil { b.log.Warn("temporary file cleanup failed","path",downloaded.Path,"error",cleanupErr) }
		done++
		_ = b.sendMessage(ctx,chatID,fmt.Sprintf("%s progress: %d/%d uploaded (%d failed, %d skipped).",label,done,len(ids),failed,skipped))
	}
	_ = b.sendMessage(ctx,chatID,fmt.Sprintf("%s finished. Uploaded: %d, failed: %d, skipped: %d, requested: %d.",label,done,failed,skipped,len(ids)))
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
