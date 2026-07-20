package login

import (
	"errors"
	"fmt"

	"game-server/internal/store"
)

var ErrInvalidPlayerRepositoryResult = errors.New("login: player repository returned nil player")

type LoginOperationStage string

const (
	LoginStageUnknown        LoginOperationStage = "unknown"
	LoginStageExchangeCode   LoginOperationStage = "exchange_code"
	LoginStageLookupPlayer   LoginOperationStage = "lookup_player"
	LoginStageCreatePlayer   LoginOperationStage = "create_player"
	LoginStageUpdateNickname LoginOperationStage = "update_nickname"
	LoginStageGenerateToken  LoginOperationStage = "generate_token"
	LoginStageUpdateToken    LoginOperationStage = "update_token"
	LoginStageLoadSession    LoginOperationStage = "load_session"
	LoginStageStoreSession   LoginOperationStage = "store_session"
)

type TokenGenerator func() (string, error)

type LoginResult struct {
	UID      int64
	Nickname string
	Token    string
}

type LoginService struct {
	players   store.PlayerRepository
	sessions  store.SessionRepository
	exchanger CodeExchanger
	generate  TokenGenerator
}

func NewLoginService(
	players store.PlayerRepository,
	sessions store.SessionRepository,
	exchanger CodeExchanger,
	generate TokenGenerator,
) *LoginService {
	return &LoginService{
		players:   players,
		sessions:  sessions,
		exchanger: exchanger,
		generate:  generate,
	}
}

func (s *LoginService) Login(code string) (LoginResult, error) {
	identity, err := s.exchanger.Exchange(code)
	if err != nil {
		return LoginResult{}, wrapLoginOperation(LoginStageExchangeCode, &exchangeError{err: err})
	}

	player, err := s.players.GetPlayerByOpenID(identity.OpenID)
	if store.IsNotFound(err) {
		player = &store.Player{OpenID: identity.OpenID}
		if err = s.players.CreatePlayer(player); err != nil {
			return LoginResult{}, wrapLoginOperation(LoginStageCreatePlayer, err)
		}
		player.Nickname = fmt.Sprintf("玩家%d", player.ID)
		if err = s.players.UpdatePlayer(player); err != nil {
			return LoginResult{}, wrapLoginOperation(LoginStageUpdateNickname, err)
		}
	} else if err != nil {
		return LoginResult{}, wrapLoginOperation(LoginStageLookupPlayer, err)
	}
	if player == nil {
		return LoginResult{}, wrapLoginOperation(LoginStageLookupPlayer, ErrInvalidPlayerRepositoryResult)
	}

	token, err := s.generate()
	if err != nil {
		return LoginResult{}, wrapLoginOperation(LoginStageGenerateToken, err)
	}
	player.Token = token
	if err = s.players.UpdatePlayer(player); err != nil {
		return LoginResult{}, wrapLoginOperation(LoginStageUpdateToken, err)
	}
	if err = s.sessions.SetSession(player.ID, &store.SessionData{
		Uid:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	}); err != nil {
		return LoginResult{}, wrapLoginOperation(LoginStageStoreSession, err)
	}

	return LoginResult{
		UID:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	}, nil
}

func (s *LoginService) RefreshSession(uid int64) error {
	data, err := s.sessions.GetSession(uid)
	if err != nil {
		return wrapLoginOperation(LoginStageLoadSession, err)
	}
	if data == nil {
		data = &store.SessionData{Uid: uid}
	}
	return wrapLoginOperation(LoginStageStoreSession, s.sessions.SetSession(uid, data))
}

type loginOperationError struct {
	stage LoginOperationStage
	cause error
}

func wrapLoginOperation(stage LoginOperationStage, err error) error {
	if err == nil {
		return nil
	}
	return &loginOperationError{stage: stage, cause: err}
}

func (e *loginOperationError) Error() string {
	return fmt.Sprintf("login operation %s failed", e.stage)
}

func (e *loginOperationError) Unwrap() error {
	return e.cause
}

func loginOperationStage(err error) LoginOperationStage {
	var operationErr *loginOperationError
	if errors.As(err, &operationErr) {
		return operationErr.stage
	}
	return LoginStageUnknown
}

type exchangeError struct {
	err error
}

func (e *exchangeError) Error() string {
	return "exchange login code failed"
}

func (e *exchangeError) Unwrap() error {
	return e.err
}

func isExchangeError(err error) bool {
	var target *exchangeError
	return errors.As(err, &target)
}
