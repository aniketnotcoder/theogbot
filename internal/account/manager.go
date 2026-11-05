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
	ErrAlreadyLoggedIn = errors.New("account already connected")
	ErrNoPendingLogin  = errors.New("no login is waiting for a code or password")
)

type prompt struct {
	value string
	err   error
}

type account struct {
	client *telegram.Client
	cancel context.CancelFunc
	code   chan prompt
	pass   chan prompt
	done   chan error
}

type Manager struct {
	appID   int
	appHash string
	mu      sync.Mutex
	users   map[int64]*account
}

func NewManager(appID int, appHash string) *Manager {
	return &Manager{appID: appID, appHash: appHash, users: make(map[int64]*account)}
}

func (m *Manager) Begin(ctx context.Context, userID int64, phone string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return errors.New("phone number is required")
	}
	m.mu.Lock()
	if _, ok := m.users[userID]; ok {
		m.mu.Unlock()
		return ErrAlreadyLoggedIn
	}
	runCtx, cancel := context.WithCancel(ctx)
	a := &account{
		client: telegram.NewClient(m.appID, m.appHash, telegram.Options{SessionStorage: &session.StorageMemory{}}),
		cancel: cancel,
		code: make(chan prompt, 1),
		pass: make(chan prompt, 1),
		done: make(chan error, 1),
	}
	m.users[userID] = a
	m.mu.Unlock()

	go func() {
		err := a.client.Run(runCtx, func(clientCtx context.Context) error {
			authenticator := interactiveAuth{phone: phone, code: a.code, pass: a.pass}
			if err := auth.NewFlow(authenticator, auth.SendCodeOptions{}).Run(clientCtx, a.client.Auth()); err != nil {\n\t\t\t\treturn err\n\t\t\t}\n\t\t\t<-clientCtx.Done()\n\t\t\treturn nil
		})
		a.done <- err
		m.mu.Lock()
		if m.users[userID] == a {
			delete(m.users, userID)
		}
		m.mu.Unlock()
	}()
	return nil
}

func (m *Manager) SubmitCode(userID int64, code string) error {
	return m.submit(userID, true, strings.TrimSpace(code))
}

func (m *Manager) SubmitPassword(userID int64, password string) error {
	return m.submit(userID, false, password)
}

func (m *Manager) submit(userID int64, isCode bool, value string) error {
	if value == "" {
		return errors.New("value cannot be empty")
	}
	m.mu.Lock()
	a := m.users[userID]
	m.mu.Unlock()
	if a == nil {
		return ErrNoPendingLogin
	}
	ch := a.pass
	if isCode {
		ch = a.code
	}
	select {
	case ch <- prompt{value: value}:
		return nil
	default:
		return errors.New("a response is already pending")
	}
}

func (m *Manager) Logout(userID int64) bool {
	m.mu.Lock()
	a := m.users[userID]
	if a != nil {
		delete(m.users, userID)
	}
	m.mu.Unlock()
	if a == nil {
		return false
	}
	a.cancel()
	return true
}

func (m *Manager) Connected(userID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.users[userID] != nil
}

type interactiveAuth struct {
	phone string
	code  <-chan prompt
	pass  <-chan prompt
}

func (a interactiveAuth) Phone(context.Context) (string, error) { return a.phone, nil }
func (a interactiveAuth) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	select {
	case p := <-a.code:
		return p.value, p.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func (a interactiveAuth) Password(ctx context.Context) (string, error) {
	select {
	case p := <-a.pass:
		return p.value, p.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
func (interactiveAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return fmt.Errorf("Telegram requires account signup; existing-account login only")
}
func (interactiveAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("new Telegram account signup is not supported")
}
