package login

import (
	"errors"
	"testing"

	"game-server/internal/store"
)

func TestLoginServiceRegistersOnlyWhenPlayerIsNotFound(t *testing.T) {
	repositoryErr := errors.New("player repository unavailable")
	players := &playerRepositoryStub{getErr: repositoryErr}
	service := NewLoginService(players, &sessionRepositoryStub{}, codeExchangerFunc(func(string) (LoginIdentity, error) {
		return LoginIdentity{OpenID: "openid-1"}, nil
	}), func() (string, error) { return "unused", nil })

	_, err := service.Login("code")
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("Login() error = %v, want repository error", err)
	}
	if players.createCalls != 0 {
		t.Fatalf("CreatePlayer() calls = %d, want 0", players.createCalls)
	}
}

func TestLoginServiceRejectsNilPlayerWithoutRegistering(t *testing.T) {
	players := &playerRepositoryStub{}
	service := NewLoginService(players, &sessionRepositoryStub{}, codeExchangerFunc(func(string) (LoginIdentity, error) {
		return LoginIdentity{OpenID: "openid-1"}, nil
	}), func() (string, error) { return "unused", nil })

	if _, err := service.Login("code"); err == nil {
		t.Fatal("Login() error = nil, want invalid repository result")
	}
	if players.createCalls != 0 {
		t.Fatalf("CreatePlayer() calls = %d, want 0", players.createCalls)
	}
}

func TestLoginServiceRegistersNotFoundPlayerAndPersistsSession(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(
		developmentStore,
		developmentStore,
		NewDevelopmentCodeExchanger(true),
		func() (string, error) { return "fixed-token", nil },
	)

	result, err := service.Login("dev:editor-001")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.UID != 1 || result.Nickname == "" || result.Token != "fixed-token" {
		t.Fatalf("Login() = %#v, want UID 1, nickname, fixed token", result)
	}

	player, err := developmentStore.GetPlayerByOpenID("dev:editor-001")
	if err != nil || player.Token != "fixed-token" || player.Nickname != result.Nickname {
		t.Fatalf("stored player = %#v, %v; want persisted identity and token", player, err)
	}
	session, err := developmentStore.GetSession(result.UID)
	if err != nil || session == nil || session.Token != result.Token || session.Nickname != result.Nickname {
		t.Fatalf("stored session = %#v, %v; want login identity", session, err)
	}
}

func TestLoginServicePropagatesTokenAndSessionFailures(t *testing.T) {
	t.Run("token", func(t *testing.T) {
		tokenErr := errors.New("token entropy unavailable")
		developmentStore := store.NewMemoryDevelopmentStore()
		service := NewLoginService(developmentStore, developmentStore, NewDevelopmentCodeExchanger(true), func() (string, error) {
			return "", tokenErr
		})

		if _, err := service.Login("dev:editor-001"); !errors.Is(err, tokenErr) {
			t.Fatalf("Login() error = %v, want token generator error", err)
		}
		if session, err := developmentStore.GetSession(1); err != nil || session != nil {
			t.Fatalf("GetSession() = %#v, %v; want nil, nil", session, err)
		}
	})

	t.Run("session", func(t *testing.T) {
		sessionErr := errors.New("session repository unavailable")
		developmentStore := store.NewMemoryDevelopmentStore()
		sessions := &sessionRepositoryStub{setErr: sessionErr}
		service := NewLoginService(developmentStore, sessions, NewDevelopmentCodeExchanger(true), func() (string, error) {
			return "fixed-token", nil
		})

		if _, err := service.Login("dev:editor-001"); !errors.Is(err, sessionErr) {
			t.Fatalf("Login() error = %v, want session repository error", err)
		}
	})
}

func TestLoginServiceWrapsCodeExchangeFailure(t *testing.T) {
	exchangeErr := errors.New("wechat unavailable")
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(developmentStore, developmentStore, codeExchangerFunc(func(string) (LoginIdentity, error) {
		return LoginIdentity{}, exchangeErr
	}), func() (string, error) { return "unused", nil })

	_, err := service.Login("code")
	if !isExchangeError(err) || !errors.Is(err, exchangeErr) {
		t.Fatalf("Login() error = %T %v, want wrapped exchange error", err, err)
	}
}

func TestLoginServiceRefreshSessionPreservesOrCreatesSession(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(developmentStore, developmentStore, nil, nil)
	existing := &store.SessionData{Uid: 7, Nickname: "player-7", Token: "token-7"}
	if err := developmentStore.SetSession(7, existing); err != nil {
		t.Fatal(err)
	}

	if err := service.RefreshSession(7); err != nil {
		t.Fatalf("RefreshSession(existing) error = %v", err)
	}
	refreshed, _ := developmentStore.GetSession(7)
	if refreshed == nil || *refreshed != *existing {
		t.Fatalf("refreshed session = %#v, want %#v", refreshed, existing)
	}

	if err := service.RefreshSession(8); err != nil {
		t.Fatalf("RefreshSession(absent) error = %v", err)
	}
	created, _ := developmentStore.GetSession(8)
	if created == nil || *created != (store.SessionData{Uid: 8}) {
		t.Fatalf("created session = %#v, want UID-only session", created)
	}
}

type codeExchangerFunc func(string) (LoginIdentity, error)

func (f codeExchangerFunc) Exchange(code string) (LoginIdentity, error) { return f(code) }

type playerRepositoryStub struct {
	getErr      error
	createCalls int
}

func (s *playerRepositoryStub) GetPlayerByID(int64) (*store.Player, error) {
	return nil, store.ErrNotFound
}
func (s *playerRepositoryStub) GetPlayerByOpenID(string) (*store.Player, error) {
	return nil, s.getErr
}
func (s *playerRepositoryStub) CreatePlayer(player *store.Player) error {
	s.createCalls++
	player.ID = 1
	return nil
}
func (s *playerRepositoryStub) UpdatePlayer(*store.Player) error { return nil }

type sessionRepositoryStub struct {
	session *store.SessionData
	getErr  error
	setErr  error
}

func (s *sessionRepositoryStub) SetSession(_ int64, data *store.SessionData) error {
	if s.setErr != nil {
		return s.setErr
	}
	copy := *data
	s.session = &copy
	return nil
}
func (s *sessionRepositoryStub) GetSession(int64) (*store.SessionData, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.session == nil {
		return nil, nil
	}
	copy := *s.session
	return &copy, nil
}
func (s *sessionRepositoryStub) DelSession(int64) error {
	s.session = nil
	return nil
}
