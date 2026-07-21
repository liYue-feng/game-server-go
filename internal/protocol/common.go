// Package protocol — 通用常量和错误码
package protocol

// ========== 错误码定义 ==========
// 命名规则：Err模块_原因
//   - 1xxxx: 通用错误
//   - 2xxxx: 登录模块错误
//   - 3xxxx: 游戏模块错误
//   - 4xxxx: 排行榜模块错误
const (
	// 通用错误
	ErrSuccess      = 0     // 成功（非错误，用于判断）
	ErrInternal     = 10001 // 服务器内部错误
	ErrInvalidParam = 10002 // 参数无效
	ErrTooFrequent  = 10003 // 请求过于频繁
	ErrUnauthorized = 10004 // 未授权（未登录或 token 过期）

	// 登录模块错误
	ErrLoginInvalidCode  = 20001 // 无效的微信登录 code
	ErrLoginWechatFailed = 20002 // 微信 API 调用失败
	ErrLoginTokenExpired = 20003 // token 已过期

	// 游戏存档模块错误
	ErrArchiveSaveFailed = 30001 // 存档保存失败
	ErrArchiveNotFound   = 30002 // 存档不存在（新玩家首次登录不算错误）

	// 排行榜模块错误
	ErrRankInvalidType  = 40001 // 无效的排行榜类型
	ErrRankInvalidRange = 40002 // 无效的排名范围

	// 战斗模块错误
	ErrCombatInvalidResult    = 50001 // 战斗结算数据无效
	ErrCombatCheatDetected    = 50002 // 检测到作弊行为
	ErrCombatConfigNotFound   = 50003 // 战斗配置不存在
	ErrCombatStyleLocked      = 50004 // 流派未解锁
	ErrCombatInsufficientGold = 50005 // 金币不足

	// Payment module errors.
	ErrPaymentUnavailable = 60001
)

// IsSuccess 判断错误码是否为成功
func IsSuccess(code int) bool {
	return code == ErrSuccess
}
