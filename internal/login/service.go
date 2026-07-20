package login

import (
	"errors"
	"fmt"

	"game-server/internal/store"
)

var ErrInvalidPlayerRepositoryResult = errors.New("login: player repository returned nil player")

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
		return LoginResult{}, &exchangeError{err: err}
	}

	player, err := s.players.GetPlayerByOpenID(identity.OpenID)
	if store.IsNotFound(err) {
		player = &store.Player{OpenID: identity.OpenID}
		if err = s.players.CreatePlayer(player); err != nil {
			return LoginResult{}, err
		}
		player.Nickname = fmt.Sprintf("玩家%d", player.ID)
		if err = s.players.UpdatePlayer(player); err != nil {
			return LoginResult{}, err
		}
	} else if err != nil {
		return LoginResult{}, err
	}
	if player == nil {
		return LoginResult{}, ErrInvalidPlayerRepositoryResult
	}

	token, err := s.generate()
	if err != nil {
		return LoginResult{}, err
	}
	player.Token = token
	if err = s.players.UpdatePlayer(player); err != nil {
		return LoginResult{}, err
	}
	if err = s.sessions.SetSession(player.ID, &store.SessionData{
		Uid:      player.ID,
		Nickname: player.Nickname,
		Token:    token,
	}); err != nil {
		return LoginResult{}, err
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
		return err
	}
	if data == nil {
		data = &store.SessionData{Uid: uid}
	}
	return s.sessions.SetSession(uid, data)
}

type exchangeError struct {
	err error
}

func (e *exchangeError) Error() string {
	return fmt.Sprintf("exchange login code: %v", e.err)
}

func (e *exchangeError) Unwrap() error {
	return e.err
}

func isExchangeError(err error) bool {
	var target *exchangeError
	return errors.As(err, &target)
}
