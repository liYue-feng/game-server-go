// Package protocol 定义服务器与客户端之间的通信协议。
//
// 协议设计思路：
//
//	为什么不用纯 JSON？
//	  - JSON 需要读取完整内容后才能解析，无法在传输中确定边界
//	  - 游戏服务器需要高吞吐，二进制帧头比 JSON 解析快得多
//
//	为什么不用 Protobuf？
//	  - 增加构建复杂度（需要 protoc 编译器）
//	  - 初期消息类型少，JSON 足够
//	  - 但协议设计为"二进制帧头+JSON载荷"，未来可以无缝替换为 Protobuf 载荷
//
// 线路格式：
//
//	+-------------------+-------------------+-------------------+
//	| Length (4 bytes)  | MsgID  (2 bytes)  | Body   (N bytes)  |
//	+-------------------+-------------------+-------------------+
//	| 小端序 uint32      | 小端序 uint16      | JSON 编码的消息体  |
//	+-------------------+-------------------+-------------------+
//
//	Length 包含自身（4字节）+ MsgID（2字节）+ Body 长度
//	所以最小 Length = 6（空消息体的情况）
package protocol

// ========== 消息ID定义 ==========
// 命名规则：MsgID_模块_动作_方向
//   - 1xxx: 登录模块
//   - 2xxx: 游戏存档模块
//   - 3xxx: 排行榜模块
//   - 5xxx: 支付模块
//   - 6xxx: GM指令模块
//   - 9xxx: 系统消息
const (
	// ---- 登录模块 ----
	MsgID_LoginReq      uint16 = 1001 // 登录请求（客户端 -> 服务器）
	MsgID_LoginResp     uint16 = 1002 // 登录响应（服务器 -> 客户端）
	MsgID_HeartbeatReq  uint16 = 1003 // 心跳请求（客户端 -> 服务器）
	MsgID_HeartbeatResp uint16 = 1004 // 心跳响应（服务器 -> 客户端）

	// ---- 游戏存档模块 ----
	MsgID_SaveArchiveReq  uint16 = 2001 // 保存存档请求
	MsgID_SaveArchiveResp uint16 = 2002 // 保存存档响应
	MsgID_LoadArchiveReq  uint16 = 2003 // 加载存档请求
	MsgID_LoadArchiveResp uint16 = 2004 // 加载存档响应

	// ---- 排行榜模块 ----
	MsgID_GetRankReq      uint16 = 3001 // 获取排行榜请求
	MsgID_GetRankResp     uint16 = 3002 // 获取排行榜响应
	MsgID_SubmitScoreReq  uint16 = 3003 // 提交分数请求
	MsgID_SubmitScoreResp uint16 = 3004 // 提交分数响应

	// ---- 支付模块 ----
	MsgID_CreateOrderReq  uint16 = 5001 // 创建订单请求
	MsgID_CreateOrderResp uint16 = 5002 // 创建订单响应
	MsgID_PayResultNotify uint16 = 5003 // 支付结果通知（服务器主动推送）

	// ---- GM指令模块 ----
	MsgID_GMCommandReq    uint16 = 6001 // GM指令请求
	MsgID_GMCommandResp   uint16 = 6002 // GM指令响应

	// ---- 系统消息 ----
	MsgID_Error uint16 = 9999 // 通用错误消息
)

// ========== 消息体结构定义 ==========

// LoginReq 登录请求 —— 微信小游戏登录流程：
// 1. 客户端调用 wx.login() 获取临时 code
// 2. 将 code 发给服务器
// 3. 服务器用 code 向微信 API 换取 openid + session_key
type LoginReq struct {
	Code string `json:"code"` // 微信登录临时凭证，有效期5分钟
}

// LoginResp 登录响应
type LoginResp struct {
	Uid      int64  `json:"uid"`      // 服务器分配的用户唯一ID
	Nickname string `json:"nickname"` // 玩家昵称（首次登录默认为"玩家"+uid）
	Token    string `json:"token"`    // 会话令牌，后续请求需携带
}

// HeartbeatReq 心跳请求 —— 保持连接活跃
// 客户端应每 30 秒发送一次，超过 90 秒未收到心跳则服务器断开连接
type HeartbeatReq struct {
	Timestamp int64 `json:"timestamp"` // 客户端当前时间戳（毫秒）
}

// HeartbeatResp 心跳响应
type HeartbeatResp struct {
	Timestamp int64 `json:"timestamp"` // 服务器当前时间戳（毫秒）
}

// SaveArchiveReq 保存存档请求
// 吸血鬼幸存者类游戏的存档包含：解锁的角色/武器、历史最高分、金币等
// 游戏过程中的实时状态由客户端维护，服务器只保存结算后的持久数据
type SaveArchiveReq struct {
	Data string `json:"data"` // 存档数据，JSON 字符串格式，具体结构由客户端定义
}

// SaveArchiveResp 保存存档响应
type SaveArchiveResp struct {
	Success bool `json:"success"` // 是否保存成功
}

// LoadArchiveReq 加载存档请求
// 玩家上线或重新进入游戏时调用
type LoadArchiveReq struct{}

// LoadArchiveResp 加载存档响应
type LoadArchiveResp struct {
	Data string `json:"data"` // 存档数据，JSON 字符串格式。首次登录为空字符串
}

// GetRankReq 获取排行榜请求
type GetRankReq struct {
	RankType int `json:"rank_type"` // 排行榜类型：1=最高分 2=击杀数（可扩展）
	Start    int `json:"start"`     // 起始排名（从0开始）
	Count    int `json:"count"`     // 请求数量（建议不超过100）
}

// RankItem 排行榜单条记录
type RankItem struct {
	Uid      int64  `json:"uid"`      // 用户ID
	Nickname string `json:"nickname"` // 昵称
	Score    int64  `json:"score"`    // 分数
	Rank     int    `json:"rank"`     // 排名
}

// GetRankResp 获取排行榜响应
type GetRankResp struct {
	Ranks []RankItem `json:"ranks"` // 排行榜列表
}

// SubmitScoreReq 提交分数请求
// 每局游戏结束时，客户端提交本局分数
type SubmitScoreReq struct {
	Score    int64  `json:"score"`    // 本局分数
	Metadata string `json:"metadata"` // 附加数据（如存活时间、击杀数等），JSON格式
}

// SubmitScoreResp 提交分数响应
type SubmitScoreResp struct {
	Success   bool  `json:"success"`    // 是否提交成功
	BestScore int64 `json:"best_score"` // 该玩家的历史最高分
}

// ErrorResp 通用错误响应
// 所有模块的异常都通过此消息返回，客户端根据 code 展示对应的提示
type ErrorResp struct {
	Code int    `json:"code"` // 错误码：见 common.go 中的 ErrXxx 常量
	Msg  string `json:"msg"`  // 错误描述（仅开发环境返回详细信息，生产环境应隐藏）
}
