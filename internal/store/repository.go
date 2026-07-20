package store

import (
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("store: not found")

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound)
}

type PlayerRepository interface {
	GetPlayerByID(int64) (*Player, error)
	GetPlayerByOpenID(string) (*Player, error)
	CreatePlayer(*Player) error
	UpdatePlayer(*Player) error
}

type SessionRepository interface {
	SetSession(int64, *SessionData) error
	GetSession(int64) (*SessionData, error)
	DelSession(int64) error
}

type ArchiveRepository interface {
	GetArchive(int64) (*Archive, error)
	SaveArchive(*Archive) error
}

var (
	_ PlayerRepository  = (*MySQLStore)(nil)
	_ SessionRepository = (*RedisStore)(nil)
	_ ArchiveRepository = (*MySQLStore)(nil)
)
