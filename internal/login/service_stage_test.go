package login

import (
	"errors"
	"strings"
	"testing"

	"game-server/internal/store"
)

func TestLoginServiceFailureStagesAreSafeAndUnwrapCauses(t *testing.T) {
	cause := errors.New("sensitive-operation-detail")
	successfulExchange := codeExchangerFunc(func(string) (LoginIdentity, error) {
		return LoginIdentity{OpenID: "openid-1"}, nil
	})
	existingPlayer := func() *loginStagePlayerRepository {
		return &loginStagePlayerRepository{player: &store.Player{ID: 1, OpenID: "openid-1", Nickname: "player-1"}}
	}

	tests := []struct {
		name         string
		wantStage    LoginOperationStage
		wantExchange bool
		service      func() *LoginService
	}{
		{
			name:         "exchange code",
			wantStage:    LoginStageExchangeCode,
			wantExchange: true,
			service: func() *LoginService {
				return NewLoginService(existingPlayer(), &sessionRepositoryStub{}, codeExchangerFunc(func(string) (LoginIdentity, error) {
					return LoginIdentity{}, cause
				}), func() (string, error) { return "token", nil })
			},
		},
		{
			name:      "lookup player",
			wantStage: LoginStageLookupPlayer,
			service: func() *LoginService {
				return NewLoginService(&loginStagePlayerRepository{getErr: cause}, &sessionRepositoryStub{}, successfulExchange, func() (string, error) { return "token", nil })
			},
		},
		{
			name:      "create player",
			wantStage: LoginStageCreatePlayer,
			service: func() *LoginService {
				return NewLoginService(&loginStagePlayerRepository{getErr: store.ErrNotFound, createErr: cause}, &sessionRepositoryStub{}, successfulExchange, func() (string, error) { return "token", nil })
			},
		},
		{
			name:      "update nickname",
			wantStage: LoginStageUpdateNickname,
			service: func() *LoginService {
				return NewLoginService(&loginStagePlayerRepository{getErr: store.ErrNotFound, updateErrAt: map[int]error{1: cause}}, &sessionRepositoryStub{}, successfulExchange, func() (string, error) { return "token", nil })
			},
		},
		{
			name:      "generate token",
			wantStage: LoginStageGenerateToken,
			service: func() *LoginService {
				return NewLoginService(existingPlayer(), &sessionRepositoryStub{}, successfulExchange, func() (string, error) { return "", cause })
			},
		},
		{
			name:      "update token",
			wantStage: LoginStageUpdateToken,
			service: func() *LoginService {
				players := existingPlayer()
				players.updateErrAt = map[int]error{1: cause}
				return NewLoginService(players, &sessionRepositoryStub{}, successfulExchange, func() (string, error) { return "token", nil })
			},
		},
		{
			name:      "store session",
			wantStage: LoginStageStoreSession,
			service: func() *LoginService {
				return NewLoginService(existingPlayer(), &sessionRepositoryStub{setErr: cause}, successfulExchange, func() (string, error) { return "token", nil })
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.service().Login("code")
			if got := loginOperationStage(err); got != tt.wantStage {
				t.Fatalf("loginOperationStage() = %q, want %q; error = %v", got, tt.wantStage, err)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("Login() error = %v, want wrapped cause", err)
			}
			if strings.Contains(err.Error(), cause.Error()) {
				t.Fatalf("Login() safe error leaked cause: %v", err)
			}
			if got := isExchangeError(err); got != tt.wantExchange {
				t.Fatalf("isExchangeError() = %v, want %v", got, tt.wantExchange)
			}
		})
	}
}

func TestLoginServiceRefreshSessionFailureStagesAreSafe(t *testing.T) {
	cause := errors.New("sensitive-session-detail")
	tests := []struct {
		name      string
		sessions  *sessionRepositoryStub
		wantStage LoginOperationStage
	}{
		{name: "load session", sessions: &sessionRepositoryStub{getErr: cause}, wantStage: LoginStageLoadSession},
		{name: "store session", sessions: &sessionRepositoryStub{setErr: cause}, wantStage: LoginStageStoreSession},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewLoginService(nil, tt.sessions, nil, nil)
			err := service.RefreshSession(7)
			if got := loginOperationStage(err); got != tt.wantStage {
				t.Fatalf("loginOperationStage() = %q, want %q; error = %v", got, tt.wantStage, err)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("RefreshSession() error = %v, want wrapped cause", err)
			}
			if strings.Contains(err.Error(), cause.Error()) {
				t.Fatalf("RefreshSession() safe error leaked cause: %v", err)
			}
		})
	}
}

type loginStagePlayerRepository struct {
	player      *store.Player
	getErr      error
	createErr   error
	updateErrAt map[int]error
	updateCalls int
}

func (r *loginStagePlayerRepository) GetPlayerByID(int64) (*store.Player, error) {
	return nil, store.ErrNotFound
}

func (r *loginStagePlayerRepository) GetPlayerByOpenID(string) (*store.Player, error) {
	return r.player, r.getErr
}

func (r *loginStagePlayerRepository) CreatePlayer(player *store.Player) error {
	if r.createErr != nil {
		return r.createErr
	}
	player.ID = 1
	return nil
}

func (r *loginStagePlayerRepository) UpdatePlayer(*store.Player) error {
	r.updateCalls++
	return r.updateErrAt[r.updateCalls]
}
