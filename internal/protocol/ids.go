package protocol

const (
	MsgID_LoginReq              uint16 = 1001
	MsgID_LoginResp             uint16 = 1002
	MsgID_HeartbeatReq          uint16 = 1003
	MsgID_HeartbeatResp         uint16 = 1004
	MsgID_SaveArchiveReq        uint16 = 2001
	MsgID_SaveArchiveResp       uint16 = 2002
	MsgID_LoadArchiveReq        uint16 = 2003
	MsgID_LoadArchiveResp       uint16 = 2004
	MsgID_GetRankReq            uint16 = 3001
	MsgID_GetRankResp           uint16 = 3002
	MsgID_SubmitScoreReq        uint16 = 3003
	MsgID_SubmitScoreResp       uint16 = 3004
	MsgID_CombatResultReq       uint16 = 4001
	MsgID_CombatResultResp      uint16 = 4002
	MsgID_GetEnemyConfigsReq    uint16 = 4003
	MsgID_GetEnemyConfigsResp   uint16 = 4004
	MsgID_GetDungeonConfigReq   uint16 = 4005
	MsgID_GetDungeonConfigResp  uint16 = 4006
	MsgID_GetStyleConfigsReq    uint16 = 4007
	MsgID_GetStyleConfigsResp   uint16 = 4008
	MsgID_UnlockStyleReq        uint16 = 4009
	MsgID_UnlockStyleResp       uint16 = 4010
	MsgID_GetPlayerStatsReq     uint16 = 4011
	MsgID_GetPlayerStatsResp    uint16 = 4012
	MsgID_UpdatePlayerStatsReq  uint16 = 4013
	MsgID_UpdatePlayerStatsResp uint16 = 4014
	MsgID_CreateOrderReq        uint16 = 5001
	MsgID_CreateOrderResp       uint16 = 5002
	MsgID_PayResultNotify       uint16 = 5003
	MsgID_GMCommandReq          uint16 = 6001
	MsgID_GMCommandResp         uint16 = 6002
	MsgID_Error                 uint16 = 9999
)
