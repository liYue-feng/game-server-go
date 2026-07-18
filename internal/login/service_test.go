package login

import (
	"errors"
	"testing"

	"game-server/internal/store"
)

func TestLoginServiceLogsDevelopmentIdentityIntoStoredPlayerAndSession(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(
		developmentStore,
		developmentStore,
		NewDevelopmentCodeExchanger(true),
		func() (string, error) { return "fixed-token", nil },
	)

	for attempt := 1; attempt <= 2; attempt++ {
		result, err := service.Login("dev:editor-001")
		if err != nil {
			t.Fatalf("Login(attempt %d) error = %v", attempt, err)
		}
		if result.UID != 1 || result.Nickname == "" || result.Token != "fixed-token" {
			t.Fatalf("Login(attempt %d) = %#v, want UID 1, nonempty nickname, fixed token", attempt, result)
		}
	}

	player, err := developmentStore.GetPlayerByOpenID("dev:editor-001")
	if err != nil {
		t.Fatalf("GetPlayerByOpenID() error = %v", err)
	}
	if player.ID != 1 || player.Nickname == "" || player.Token != "fixed-token" {
		t.Fatalf("stored player = %#v, want ID 1, nonempty nickname, fixed token", player)
	}

	session, err := developmentStore.GetSession(1)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if session == nil || session.Uid != 1 || session.Nickname != player.Nickname || session.Token != "fixed-token" {
		t.Fatalf("stored session = %#v, want player identity and fixed token", session)
	}
}

func TestLoginServicePropagatesTokenGeneratorFailureWithoutSession(t *testing.T) {
	tokenErr := errors.New("token entropy unavailable")
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(
		developmentStore,
		developmentStore,
		NewDevelopmentCodeExchanger(true),
		func() (string, error) { return "", tokenErr },
	)

	if _, err := service.Login("dev:editor-001"); !errors.Is(err, tokenErr) {
		t.Fatalf("Login() error = %v, want token generator error", err)
	}
	if session, err := developmentStore.GetSession(1); err != nil || session != nil {
		t.Fatalf("GetSession() = %#v, %v; want nil, nil", session, err)
	}
}

func TestLoginServiceClassifiesAndUnwrapsExchangeFailure(t *testing.T) {
	exchangeErr := errors.New("credential exchange unavailable")
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(
		developmentStore,
		developmentStore,
		codeExchangerFunc(func(string) (LoginIdentity, error) {
			return LoginIdentity{}, exchangeErr
		}),
		func() (string, error) { return "unused", nil },
	)

	_, err := service.Login("invalid-code")
	if !isExchangeError(err) {
		t.Fatalf("Login() error = %T %v, want exchange error classification", err, err)
	}
	if !errors.Is(err, exchangeErr) {
		t.Fatalf("Login() error = %v, want wrapped original exchange error", err)
	}
}

func TestLoginServiceReturnsSessionWriteFailure(t *testing.T) {
	sessionErr := errors.New("session storage unavailable")
	developmentStore := store.NewMemoryDevelopmentStore()
	sessions := &sessionRepositoryStub{setErr: sessionErr}
	service := NewLoginService(
		developmentStore,
		sessions,
		NewDevelopmentCodeExchanger(true),
		func() (string, error) { return "fixed-token", nil },
	)

	if _, err := service.Login("dev:editor-001"); !errors.Is(err, sessionErr) {
		t.Fatalf("Login() error = %v, want session write error", err)
	}
	if sessions.session != nil {
		t.Fatalf("stored session = %#v, want no session after write failure", sessions.session)
	}
}

func TestLoginServiceRefreshSessionPreservesExistingAndCreatesAbsent(t *testing.T) {
	developmentStore := store.NewMemoryDevelopmentStore()
	service := NewLoginService(developmentStore, developmentStore, nil, nil)
	existing := &store.SessionData{Uid: 7, Nickname: "player-7", Token: "token-7"}
	if err := developmentStore.SetSession(7, existing); err != nil {
		t.Fatalf("SetSession(existing) error = %v", err)
	}

	if err := service.RefreshSession(7); err != nil {
		t.Fatalf("RefreshSession(existing) error = %v", err)
	}
	refreshed, err := developmentStore.GetSession(7)
	if err != nil {
		t.Fatalf("GetSession(existing) error = %v", err)
	}
	if refreshed == nil || *refreshed != *existing {
		t.Fatalf("refreshed session = %#v, want %#v", refreshed, existing)
	}

	if err := service.RefreshSession(8); err != nil {
		t.Fatalf("RefreshSession(absent) error = %v", err)
	}
	created, err := developmentStore.GetSession(8)
	if err != nil {
		t.Fatalf("GetSession(created) error = %v", err)
	}
	if created == nil || *created != (store.SessionData{Uid: 8}) {
		t.Fatalf("created session = %#v, want UID-only session", created)
	}
}

type codeExchangerFunc func(string) (LoginIdentity, error)

func (f codeExchangerFunc) Exchange(code string) (LoginIdentity, error) {
	return f(code)
}

type sessionRepositoryStub struct {
	session *store.SessionData
	setErr  error
}

func (s *sessionRepositoryStub) SetSession(_ int64, session *store.SessionData) error {
	if s.setErr != nil {
		return s.setErr
	}
	copy := *session
	s.session = &copy
	return nil
}

func (s *sessionRepositoryStub) GetSession(int64) (*store.SessionData, error) {
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
