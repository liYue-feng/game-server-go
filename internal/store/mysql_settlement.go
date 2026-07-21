package store

import (
	"errors"
	"fmt"
	"math"

	"game-server/internal/model"
	"game-server/internal/protocolpb"

	drivermysql "github.com/go-sql-driver/mysql"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errDuplicateCombatSettlement = errors.New("store: duplicate combat settlement")

// MySQLCombatSettlementRepository owns the transaction boundary for one run.
type MySQLCombatSettlementRepository struct {
	db     *gorm.DB
	policy CombatRewardPolicy
}

func NewMySQLCombatSettlementRepository(mysqlStore *MySQLStore, policy CombatRewardPolicy) *MySQLCombatSettlementRepository {
	if mysqlStore == nil || mysqlStore.db == nil {
		return nil
	}
	if err := validateCombatRewardPolicy(policy); err != nil {
		panic(err)
	}
	return &MySQLCombatSettlementRepository{db: mysqlStore.db, policy: policy}
}

func (r *MySQLCombatSettlementRepository) Settle(playerID int64, req *protocolpb.CombatResultReq) (*protocolpb.CombatResultResp, error) {
	if r == nil || r.db == nil || req == nil {
		return nil, fmt.Errorf("settle combat: invalid repository or request")
	}

	var response *protocolpb.CombatResultResp
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var player model.Player
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", playerID).First(&player).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("player %d: %w", playerID, ErrNotFound)
			}
			return err
		}
		var existing model.CombatSettlement
		err := tx.Where("player_id = ? AND run_id = ?", playerID, req.RunId).First(&existing).Error
		if err == nil {
			stored, decodeErr := decodeSettlementResponse(existing.Response)
			if decodeErr != nil {
				return decodeErr
			}
			stored.Duplicate = true
			stored.RunId = req.RunId
			response = stored
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		archive, err := settlementArchiveFromTransaction(tx, playerID)
		if err != nil {
			return err
		}
		response, err = settleArchive(archive, req, r.policy)
		if err != nil {
			return err
		}
		if err := updateSettlementStats(tx, playerID, req, response); err != nil {
			return err
		}
		metadata, err := CombatScoreMetadataJSON(req)
		if err != nil {
			return err
		}
		if err := tx.Create(&model.ScoreRecord{
			PlayerID: playerID,
			Score:    req.Score,
			Metadata: metadata,
		}).Error; err != nil {
			return err
		}
		archiveBytes, err := proto.Marshal(response.Archive)
		if err != nil {
			return fmt.Errorf("encode settlement archive: %w", err)
		}
		if err := saveArchiveQuery(tx, &model.Archive{PlayerID: playerID, Data: archiveBytes}).Error; err != nil {
			return err
		}
		responseBytes, err := proto.Marshal(response)
		if err != nil {
			return fmt.Errorf("encode settlement response: %w", err)
		}
		if err := tx.Create(&model.CombatSettlement{PlayerID: playerID, RunID: req.RunId, Response: responseBytes}).Error; err != nil {
			if isDuplicateKey(err) {
				return errDuplicateCombatSettlement
			}
			return err
		}
		return nil
	})
	if errors.Is(err, errDuplicateCombatSettlement) {
		var existing model.CombatSettlement
		if lookupErr := r.db.Where("player_id = ? AND run_id = ?", playerID, req.RunId).First(&existing).Error; lookupErr != nil {
			return nil, lookupErr
		}
		stored, decodeErr := decodeSettlementResponse(existing.Response)
		if decodeErr != nil {
			return nil, decodeErr
		}
		stored.Duplicate = true
		stored.RunId = req.RunId
		return stored, nil
	}
	if err != nil {
		return nil, err
	}
	return response, nil
}

func settlementArchiveFromTransaction(tx *gorm.DB, playerID int64) (*protocolpb.PlayerArchive, error) {
	var stored model.Archive
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ?", playerID).First(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &protocolpb.PlayerArchive{SchemaVersion: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	archive := &protocolpb.PlayerArchive{}
	if err := proto.Unmarshal(stored.Data, archive); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedSettlementArchive, err)
	}
	return archive, nil
}

func updateSettlementStats(tx *gorm.DB, playerID int64, req *protocolpb.CombatResultReq, response *protocolpb.CombatResultResp) error {
	var stats model.PlayerStats
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ?", playerID).First(&stats).Error
	created := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		stats = model.PlayerStats{PlayerID: playerID, Level: int(req.PlayerLevel), MaxHp: 100, MaxStamina: 100, AttackPower: 10}
		created = true
	} else if err != nil {
		return err
	}
	if int64(stats.Gold)+int64(response.RewardGold) > math.MaxInt || int64(stats.Exp)+int64(response.RewardExp) > math.MaxInt {
		return fmt.Errorf("store: player stats reward overflow")
	}
	stats.Gold += int(response.RewardGold)
	stats.Exp += int(response.RewardExp)
	if int64(req.Kills) > math.MaxInt64-stats.TotalKills || stats.TotalGames == math.MaxInt64 {
		return fmt.Errorf("store: player stats totals overflow")
	}
	stats.TotalKills += int64(req.Kills)
	stats.TotalGames++
	if req.Score > stats.BestScore {
		stats.BestScore = req.Score
	}
	if req.PlayerLevel > int32(stats.Level) {
		stats.Level = int(req.PlayerLevel)
	}
	if created {
		return tx.Create(&stats).Error
	}
	return tx.Save(&stats).Error
}

func decodeSettlementResponse(data []byte) (*protocolpb.CombatResultResp, error) {
	response := &protocolpb.CombatResultResp{}
	if err := proto.Unmarshal(data, response); err != nil {
		return nil, fmt.Errorf("decode stored settlement response: %w", err)
	}
	return response, nil
}

func isDuplicateKey(err error) bool {
	var mysqlErr *drivermysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

var _ CombatSettlementRepository = (*MySQLCombatSettlementRepository)(nil)
