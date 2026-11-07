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
)

type prompt struct{ value string }
type account struct {
	cancel context.CancelFunc
	code chan prompt
	pass chan prompt
	state string
	lastError string
	client *telegram.Client
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
	a:=&account{cancel:cancel,code:make(chan prompt,1),pass:make(chan prompt,1),state:"starting"}
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
func(m *Manager)API(userID int64)(*tg.Client,bool){
	m.mu.Lock();defer m.mu.Unlock()
	a:=m.users[userID]
	if a==nil||a.state!="connected"||a.client==nil{return nil,false}
	return a.client.API(),true
}
