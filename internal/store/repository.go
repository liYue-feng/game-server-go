package store

import (
	"errors"

	"game-server/internal/protocolpb"

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

// CombatRewardPolicy is fixed when a settlement repository is constructed.
type CombatRewardPolicy struct {
	GoldPerKill int
	ExpPerKill  int
}

// CombatSettlementRepository atomically settles one player run by its run ID.
type CombatSettlementRepository interface {
	Settle(int64, *protocolpb.CombatResultReq) (*protocolpb.CombatResultResp, error)
}

type DevelopmentPlayerStatsRepository interface {
	GetDevelopmentPlayerLevel(int64) (int32, error)
}

var (
	_ PlayerRepository  = (*MySQLStore)(nil)
	_ SessionRepository = (*RedisStore)(nil)
	_ ArchiveRepository = (*MySQLStore)(nil)
)
