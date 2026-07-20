// Package protocol —— 业务错误类型 BizError
//
// 为什么需要 BizError？
//
//	改造后业务 handler 的签名统一为 func(ctx, *Req) (*Resp, error)，
//	内核（kernel）需要区分两类错误：
//	  1. 业务错误：可预期、要回传给客户端具体错误码（如"code 无效""余额不足"）
//	  2. 系统错误：不可预期（如 DB 挂了），统一回 ErrInternal，避免泄露内部细节
//
//	BizError 就是第 1 类的载体：它包裹 Code + Msg，实现 error 接口，
//	内核识别到它就编码为 MsgID_Error 帧（复用现有 ErrorResp 结构），
//	识别到普通 error 则记日志并回 ErrInternal。
//
//	这样业务代码只需 return protocol.NewBizError(code, msg)，
//	不必自己组装错误响应帧，与 pitaya 的 pitaya.Error 语义对齐。
package protocol

// BizError 业务错误：携带错误码与提示信息，可安全回传客户端
type BizError struct {
	Code int    // 错误码，对应 common.go 中的 ErrXxx 常量
	Msg  string // 面向客户端的提示信息
}

// Error 实现 error 接口，使 BizError 可作为普通 error 传递
func (e *BizError) Error() string {
	return e.Msg
}

// NewBizError 构造业务错误
//
// 使用方式：
//
//	return nil, protocol.NewBizError(protocol.ErrLoginWechatFailed, "微信登录失败")
func NewBizError(code int, msg string) *BizError {
	return &BizError{Code: code, Msg: msg}
}
