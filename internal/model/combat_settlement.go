package model

import "time"

// CombatSettlement is the durable idempotency record for a completed combat run.
type CombatSettlement struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	PlayerID  int64     `gorm:"uniqueIndex:idx_player_run;not null"`
	RunID     string    `gorm:"uniqueIndex:idx_player_run;size:128;not null"`
	Response  []byte    `gorm:"type:blob;not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (CombatSettlement) TableName() string { return "combat_settlements" }
