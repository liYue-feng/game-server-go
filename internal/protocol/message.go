package protocol

import "game-server/internal/protocolpb"

// These aliases retain source compatibility without retaining hand-written
// payload definitions. The generated protocolpb package owns every body type.
type (
	LoginReq              = protocolpb.LoginReq
	LoginResp             = protocolpb.LoginResp
	HeartbeatReq          = protocolpb.HeartbeatReq
	HeartbeatResp         = protocolpb.HeartbeatResp
	PlayerArchive         = protocolpb.PlayerArchive
	SaveArchiveReq        = protocolpb.SaveArchiveReq
	SaveArchiveResp       = protocolpb.SaveArchiveResp
	LoadArchiveReq        = protocolpb.LoadArchiveReq
	LoadArchiveResp       = protocolpb.LoadArchiveResp
	GetRankReq            = protocolpb.GetRankReq
	RankItem              = protocolpb.RankItem
	GetRankResp           = protocolpb.GetRankResp
	ScoreMetadata         = protocolpb.ScoreMetadata
	SubmitScoreReq        = protocolpb.SubmitScoreReq
	SubmitScoreResp       = protocolpb.SubmitScoreResp
	CombatResultReq       = protocolpb.CombatResultReq
	CombatResultResp      = protocolpb.CombatResultResp
	EnemyConfigItem       = protocolpb.EnemyConfigItem
	GetEnemyConfigsReq    = protocolpb.GetEnemyConfigsReq
	GetEnemyConfigsResp   = protocolpb.GetEnemyConfigsResp
	DungeonConfigItem     = protocolpb.DungeonConfigItem
	GetDungeonConfigReq   = protocolpb.GetDungeonConfigReq
	GetDungeonConfigResp  = protocolpb.GetDungeonConfigResp
	StyleConfigItem       = protocolpb.StyleConfigItem
	GetStyleConfigsReq    = protocolpb.GetStyleConfigsReq
	GetStyleConfigsResp   = protocolpb.GetStyleConfigsResp
	UnlockStyleReq        = protocolpb.UnlockStyleReq
	UnlockStyleResp       = protocolpb.UnlockStyleResp
	GetPlayerStatsReq     = protocolpb.GetPlayerStatsReq
	GetPlayerStatsResp    = protocolpb.GetPlayerStatsResp
	UpdatePlayerStatsReq  = protocolpb.UpdatePlayerStatsReq
	UpdatePlayerStatsResp = protocolpb.UpdatePlayerStatsResp
	CreateOrderReq        = protocolpb.CreateOrderReq
	CreateOrderResp       = protocolpb.CreateOrderResp
	PayResultNotify       = protocolpb.PayResultNotify
	GMCommandReq          = protocolpb.GMCommandReq
	GMCommandResp         = protocolpb.GMCommandResp
	ErrorResp             = protocolpb.ErrorResp
)
