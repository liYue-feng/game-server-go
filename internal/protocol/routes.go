package protocol

import (
	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

type Route struct {
	RequestID         uint16
	ResponseID        uint16
	RequestPrototype  proto.Message
	ResponsePrototype proto.Message
}

var canonicalRoutes = []Route{
	{MsgID_LoginReq, MsgID_LoginResp, &protocolpb.LoginReq{}, &protocolpb.LoginResp{}},
	{MsgID_HeartbeatReq, MsgID_HeartbeatResp, &protocolpb.HeartbeatReq{}, &protocolpb.HeartbeatResp{}},
	{MsgID_SaveArchiveReq, MsgID_SaveArchiveResp, &protocolpb.SaveArchiveReq{}, &protocolpb.SaveArchiveResp{}},
	{MsgID_LoadArchiveReq, MsgID_LoadArchiveResp, &protocolpb.LoadArchiveReq{}, &protocolpb.LoadArchiveResp{}},
	{MsgID_GetRankReq, MsgID_GetRankResp, &protocolpb.GetRankReq{}, &protocolpb.GetRankResp{}},
	{MsgID_SubmitScoreReq, MsgID_SubmitScoreResp, &protocolpb.SubmitScoreReq{}, &protocolpb.SubmitScoreResp{}},
	{MsgID_CombatResultReq, MsgID_CombatResultResp, &protocolpb.CombatResultReq{}, &protocolpb.CombatResultResp{}},
	{MsgID_GetEnemyConfigsReq, MsgID_GetEnemyConfigsResp, &protocolpb.GetEnemyConfigsReq{}, &protocolpb.GetEnemyConfigsResp{}},
	{MsgID_GetDungeonConfigReq, MsgID_GetDungeonConfigResp, &protocolpb.GetDungeonConfigReq{}, &protocolpb.GetDungeonConfigResp{}},
	{MsgID_GetStyleConfigsReq, MsgID_GetStyleConfigsResp, &protocolpb.GetStyleConfigsReq{}, &protocolpb.GetStyleConfigsResp{}},
	{MsgID_UnlockStyleReq, MsgID_UnlockStyleResp, &protocolpb.UnlockStyleReq{}, &protocolpb.UnlockStyleResp{}},
	{MsgID_GetPlayerStatsReq, MsgID_GetPlayerStatsResp, &protocolpb.GetPlayerStatsReq{}, &protocolpb.GetPlayerStatsResp{}},
	{MsgID_UpdatePlayerStatsReq, MsgID_UpdatePlayerStatsResp, &protocolpb.UpdatePlayerStatsReq{}, &protocolpb.UpdatePlayerStatsResp{}},
	{MsgID_CreateOrderReq, MsgID_CreateOrderResp, &protocolpb.CreateOrderReq{}, &protocolpb.CreateOrderResp{}},
	{MsgID_GMCommandReq, MsgID_GMCommandResp, &protocolpb.GMCommandReq{}, &protocolpb.GMCommandResp{}},
}

var allMessageIDs = []uint16{
	MsgID_LoginReq, MsgID_LoginResp, MsgID_HeartbeatReq, MsgID_HeartbeatResp,
	MsgID_SaveArchiveReq, MsgID_SaveArchiveResp, MsgID_LoadArchiveReq, MsgID_LoadArchiveResp,
	MsgID_GetRankReq, MsgID_GetRankResp, MsgID_SubmitScoreReq, MsgID_SubmitScoreResp,
	MsgID_CombatResultReq, MsgID_CombatResultResp, MsgID_GetEnemyConfigsReq, MsgID_GetEnemyConfigsResp,
	MsgID_GetDungeonConfigReq, MsgID_GetDungeonConfigResp, MsgID_GetStyleConfigsReq, MsgID_GetStyleConfigsResp,
	MsgID_UnlockStyleReq, MsgID_UnlockStyleResp, MsgID_GetPlayerStatsReq, MsgID_GetPlayerStatsResp,
	MsgID_UpdatePlayerStatsReq, MsgID_UpdatePlayerStatsResp, MsgID_CreateOrderReq, MsgID_CreateOrderResp,
	MsgID_PayResultNotify, MsgID_GMCommandReq, MsgID_GMCommandResp, MsgID_Error,
}

func Routes() []Route         { return append([]Route(nil), canonicalRoutes...) }
func AllMessageIDs() []uint16 { return append([]uint16(nil), allMessageIDs...) }
func RouteFor(requestID uint16) (Route, bool) {
	for _, route := range canonicalRoutes {
		if route.RequestID == requestID {
			return route, true
		}
	}
	return Route{}, false
}
