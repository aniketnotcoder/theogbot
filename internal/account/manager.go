package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

var (
	ErrAlreadyLoggedIn = errors.New("an account session or login is already active")
	ErrNoPendingLogin = errors.New("no login is waiting for a code or password")
	ErrUnexpectedLoginInput = errors.New("Telegram is not currently waiting for this response")
	ErrLoginNotFailed = errors.New("the previous login attempt has not failed")
)

type prompt struct{ value string }
type account struct {
	cancel context.CancelFunc
	code chan prompt
	pass chan prompt
	state string
	lastError string
	client *telegram.Client
	phone string
}
type Notifier func(userID int64, state string, err error)
type Manager struct {
	appID int
	appHash string
	mu sync.Mutex
	users map[int64]*account
	notify Notifier
}

func NewManager(appID int, appHash string) *Manager {
	return &Manager{appID:appID,appHash:appHash,users:make(map[int64]*account)}
}
func (m *Manager) SetNotifier(notify Notifier) {
	m.mu.Lock()
	m.notify=notify
	m.mu.Unlock()
}
func (m *Manager) emit(userID int64,a *account,state string,err error) {
	m.mu.Lock()
	if m.users[userID]!=a { m.mu.Unlock(); return }
	a.state=state
	if state=="failed" && err!=nil { a.lastError=err.Error();a.client=nil }
	notify:=m.notify
	m.mu.Unlock()
	if notify!=nil { notify(userID,state,err) }
}
func (m *Manager) Begin(ctx context.Context,userID int64,phone string) error {
	phone=strings.TrimSpace(phone)
	if phone=="" { return errors.New("phone number is required") }
	m.mu.Lock()
	if previous:=m.users[userID];previous!=nil {
		if previous.state!="failed" { m.mu.Unlock();return ErrAlreadyLoggedIn }
		delete(m.users,userID)
		previous.cancel()
	}
	runCtx,cancel:=context.WithCancel(ctx)
	a:=&account{cancel:cancel,code:make(chan prompt,1),pass:make(chan prompt,1),state:"starting",phone:phone}
	m.users[userID]=a
	m.mu.Unlock()
	client:=telegram.NewClient(m.appID,m.appHash,telegram.Options{SessionStorage:&session.StorageMemory{}})
	m.mu.Lock()
	if m.users[userID]==a { a.client=client }
	m.mu.Unlock()
	go func(){
		err:=client.Run(runCtx,func(clientCtx context.Context)error{
			authenticator:=interactiveAuth{phone:phone,code:a.code,pass:a.pass,onState:func(state string){m.emit(userID,a,state,nil)}}
			if flowErr:=auth.NewFlow(authenticator,auth.SendCodeOptions{}).Run(clientCtx,client.Auth());flowErr!=nil{return flowErr}
			m.emit(userID,a,"connected",nil)
			<-clientCtx.Done()
			return nil
		})
		if err!=nil&&runCtx.Err()==nil { m.emit(userID,a,"failed",err) }
	}()
	return nil
}
func (m *Manager) Retry(ctx context.Context,userID int64)error{
	m.mu.Lock()
	a:=m.users[userID]
	if a==nil { m.mu.Unlock();return ErrNoPendingLogin }
	if a.state!="failed" { m.mu.Unlock();return ErrLoginNotFailed }
	phone:=a.phone
	m.mu.Unlock()
	return m.Begin(ctx,userID,phone)
}
func (m *Manager) SubmitCode(userID int64,code string)error{return m.submit(userID,true,strings.TrimSpace(code))}
func (m *Manager) SubmitPassword(userID int64,password string)error{return m.submit(userID,false,password)}
func (m *Manager) submit(userID int64,isCode bool,value string)error{
	if value=="" { return errors.New("value cannot be empty") }
	m.mu.Lock()
	a:=m.users[userID]
	if a==nil { m.mu.Unlock();return ErrNoPendingLogin }
	expected:="awaiting_password";ch:=a.pass
	if isCode { expected="awaiting_code";ch=a.code }
	if a.state!=expected { m.mu.Unlock();return ErrUnexpectedLoginInput }
	m.mu.Unlock()
	select{case ch<-prompt{value:value}:return nil;default:return errors.New("a response is already pending")}
}
func (m *Manager) Logout(userID int64)bool{
	m.mu.Lock();a:=m.users[userID];if a!=nil{delete(m.users,userID)};m.mu.Unlock()
	if a==nil{return false};a.cancel();return true
}
func (m *Manager) Connected(userID int64)bool{return m.Status(userID)=="connected"}
func (m *Manager) Status(userID int64)string{
	m.mu.Lock();defer m.mu.Unlock()
	if a:=m.users[userID];a!=nil{if a.state=="failed"&&a.lastError!=""{return "failed: "+a.lastError};return a.state}
	return "disconnected"
}
type interactiveAuth struct{
	phone string
	code <-chan prompt
	pass <-chan prompt
	onState func(string)
}
func(a interactiveAuth)Phone(context.Context)(string,error){return a.phone,nil}
func(a interactiveAuth)Code(ctx context.Context,_ *tg.AuthSentCode)(string,error){
	a.onState("awaiting_code")
	select{case p:=<-a.code:return p.value,nil;case<-ctx.Done():return "",ctx.Err()}
}
func(a interactiveAuth)Password(ctx context.Context)(string,error){
	a.onState("awaiting_password")
	select{case p:=<-a.pass:return p.value,nil;case<-ctx.Done():return "",ctx.Err()}
}
func(interactiveAuth)AcceptTermsOfService(context.Context,tg.HelpTermsOfService)error{return fmt.Errorf("Telegram requires account signup; existing-account login only")}
func(interactiveAuth)SignUp(context.Context)(auth.UserInfo,error){return auth.UserInfo{},fmt.Errorf("new Telegram account signup is not supported")}

type StorageGroup struct {
	ChatID int64
	Title string
	InviteLink string
}

// CreateStorageGroup creates a private supergroup from the connected source
// account, invites the configured uploader bot, and grants it limited admin rights.
func (m *Manager) CreateStorageGroup(ctx context.Context, userID int64, botUsername, requestedTitle string) (StorageGroup, error) {
	m.mu.Lock()
	a := m.users[userID]
	if a == nil || a.state != "connected" || a.client == nil {
		m.mu.Unlock()
		return StorageGroup{}, errors.New("connect your Telegram account first")
	}
	api := a.client.API()
	m.mu.Unlock()

	botUsername = strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	if botUsername == "" { return StorageGroup{}, errors.New("the uploader bot username is unavailable") }
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: botUsername})
	if err != nil { return StorageGroup{}, fmt.Errorf("find the uploader bot: %w", err) }
	var botUser *tg.User
	for _, item := range resolved.Users {
		if user, ok := item.(*tg.User); ok && !user.Deleted && strings.EqualFold(user.Username, botUsername) {
			botUser = user
			break
		}
	}
	if botUser == nil || botUser.AccessHash == 0 { return StorageGroup{}, errors.New("could not resolve the uploader bot account") }

	title := strings.TrimSpace(requestedTitle)
	if title == "" { title = "TheOGBot Storage" }
	if len([]rune(title)) > 64 { return StorageGroup{}, errors.New("storage group name must be 64 characters or fewer") }
	created, err := api.ChannelsCreateChannel(ctx, &tg.ChannelsCreateChannelRequest{
		Megagroup: true, Title: title, About: "Private storage group for TheOGBot transfers.",
	})
	if err != nil { return StorageGroup{}, fmt.Errorf("create the storage group: %w", err) }
	var chats []tg.ChatClass
	switch value := created.(type) {
	case *tg.Updates: chats = value.Chats
	case *tg.UpdatesCombined: chats = value.Chats
	default: return StorageGroup{}, fmt.Errorf("Telegram returned an unexpected group creation response: %T", created)
	}
	var channel *tg.Channel
	for _, item := range chats {
		if candidate, ok := item.(*tg.Channel); ok && candidate.Megagroup { channel = candidate; break }
	}
	if channel == nil { return StorageGroup{}, errors.New("Telegram created the group but did not return its channel details") }
	inputChannel := &tg.InputChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}
	inputBot := &tg.InputUser{UserID: botUser.ID, AccessHash: botUser.AccessHash}
	if _, err := api.ChannelsInviteToChannel(ctx, &tg.ChannelsInviteToChannelRequest{Channel: inputChannel, Users: []tg.InputUserClass{inputBot}}); err != nil {
		return StorageGroup{}, fmt.Errorf("the group was created, but Telegram could not add the uploader bot: %w", err)
	}
	if _, err := api.ChannelsEditAdmin(ctx, &tg.ChannelsEditAdminRequest{
		Channel: inputChannel, UserID: inputBot,
		AdminRights: tg.ChatAdminRights{ChangeInfo: true}, Rank: "Storage manager",
	}); err != nil {
		return StorageGroup{}, fmt.Errorf("the uploader bot joined, but could not be promoted to admin: %w", err)
	}
	// The person chatting with the bot may be using a different Telegram
	// account from the connected source account. A private t.me/c link alone
	// will not grant that account membership, so create a real join link.
	exported, err := api.MessagesExportChatInvite(ctx, &tg.MessagesExportChatInviteRequest{
		Peer: &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash},
		Title: "TheOGBot storage access",
	})
	if err != nil {
		return StorageGroup{}, fmt.Errorf("storage group and uploader are ready, but Telegram could not create a join link: %w", err)
	}
	invite, ok := exported.(*tg.ChatInviteExported)
	if !ok || strings.TrimSpace(invite.Link) == "" {
		return StorageGroup{}, fmt.Errorf("Telegram returned an invalid storage invite link (%T)", exported)
	}
	return StorageGroup{ChatID: -1000000000000 - channel.ID, Title: title, InviteLink: invite.Link}, nil
}

func(m *Manager)API(userID int64)(*tg.Client,bool){
	m.mu.Lock();defer m.mu.Unlock()
	a:=m.users[userID]
	if a==nil||a.state!="connected"||a.client==nil{return nil,false}
	return a.client.API(),true
}
