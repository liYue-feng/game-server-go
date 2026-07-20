package combat

import (
	"fmt"
	"strings"

	"game-server/internal/protocolpb"
	"game-server/internal/store"
)

const (
	maxCombatRunIDBytes = 128
	maxPlayerLevel      = 100
	maxDungeonLevel     = 100
)

// SettlementService validates client input before delegating atomic persistence.
type SettlementService struct {
	repository store.CombatSettlementRepository
	cfg        *CombatConfig
}

func NewSettlementService(repository store.CombatSettlementRepository, cfg *CombatConfig) *SettlementService {
	return &SettlementService{repository: repository, cfg: cfg}
}

func (s *SettlementService) Settle(uid int64, req *protocolpb.CombatResultReq) (*protocolpb.CombatResultResp, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("combat: settlement repository is nil")
	}
	if err := validateCombatResult(req, s.cfg); err != nil {
		return nil, err
	}
	return s.repository.Settle(uid, req)
}

func settlementRewardPolicy(cfg *CombatConfig) store.CombatRewardPolicy {
	return store.CombatRewardPolicy{GoldPerKill: cfg.GoldPerKill, ExpPerKill: cfg.ExpPerKill}
}

func validRunID(runID string) bool {
	return strings.TrimSpace(runID) == runID && runID != "" && len(runID) <= maxCombatRunIDBytes
}
