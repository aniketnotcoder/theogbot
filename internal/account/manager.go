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
)
type prompt struct { value string }
type account struct { cancel context.CancelFunc; code chan prompt; pass chan prompt; state string }
type Manager struct { appID int; appHash string; mu sync.Mutex; users map[int64]*account }

func NewManager(appID int, appHash string) *Manager { return &Manager{appID: appID, appHash: appHash, users: make(map[int64]*account)} }

func (m *Manager) Begin(ctx context.Context, userID int64, phone string) error {
 phone = strings.TrimSpace(phone)
 if phone == "" { return errors.New("phone number is required") }
 m.mu.Lock()
 if _, exists := m.users[userID]; exists { m.mu.Unlock(); return ErrAlreadyLoggedIn }
 runCtx, cancel := context.WithCancel(ctx)
 a := &account{cancel: cancel, code: make(chan prompt, 1), pass: make(chan prompt, 1), state: "awaiting_code"}
 m.users[userID] = a
 m.mu.Unlock()
 client := telegram.NewClient(m.appID, m.appHash, telegram.Options{SessionStorage: &session.StorageMemory{}})
 go func() {
  err := client.Run(runCtx, func(clientCtx context.Context) error {
   authenticator := interactiveAuth{phone: phone, code: a.code, pass: a.pass, onState: func(state string) { m.mu.Lock(); if m.users[userID] == a { a.state = state }; m.mu.Unlock() }}
   if err := auth.NewFlow(authenticator, auth.SendCodeOptions{}).Run(clientCtx, client.Auth()); err != nil { return err }
   <-clientCtx.Done()
   return nil
  })
  if err != nil && runCtx.Err() == nil {
   m.mu.Lock(); if m.users[userID] == a { delete(m.users, userID) }; m.mu.Unlock()
  }
 }()
 return nil
}
func (m *Manager) SubmitCode(userID int64, code string) error { return m.submit(userID, true, strings.TrimSpace(code)) }
func (m *Manager) SubmitPassword(userID int64, password string) error { return m.submit(userID, false, password) }
func (m *Manager) submit(userID int64, isCode bool, value string) error {
 if value == "" { return errors.New("value cannot be empty") }
 m.mu.Lock(); a := m.users[userID]; m.mu.Unlock()
 if a == nil { return ErrNoPendingLogin }
 ch := a.pass; if isCode { ch = a.code }
 select { case ch <- prompt{value: value}: return nil; default: return errors.New("a response is already pending") }
}
func (m *Manager) Logout(userID int64) bool {
 m.mu.Lock(); a := m.users[userID]; if a != nil { delete(m.users, userID) }; m.mu.Unlock()
 if a == nil { return false }; a.cancel(); return true
}
func (m *Manager) Connected(userID int64) bool { return m.Status(userID) == "connected" }
func (m *Manager) Status(userID int64) string { m.mu.Lock(); defer m.mu.Unlock(); if a := m.users[userID]; a != nil { return a.state }; return "disconnected" }

type interactiveAuth struct { phone string; code <-chan prompt; pass <-chan prompt; onState func(string) }
func (a interactiveAuth) Phone(context.Context) (string, error) { return a.phone, nil }
func (a interactiveAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) { a.onState("awaiting_code"); select { case p := <-a.code: return p.value, nil; case <-ctx.Done(): return "", ctx.Err() } }
func (a interactiveAuth) Password(ctx context.Context) (string, error) { a.onState("awaiting_password"); select { case p := <-a.pass: return p.value, nil; case <-ctx.Done(): return "", ctx.Err() } }
func (interactiveAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error { return fmt.Errorf("Telegram requires account signup; existing-account login only") }
func (interactiveAuth) SignUp(context.Context) (auth.UserInfo, error) { return auth.UserInfo{}, fmt.Errorf("new Telegram account signup is not supported") }
