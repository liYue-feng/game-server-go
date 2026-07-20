// Package kernel 是改造后的消息处理内核，借鉴 pitaya 的 HandlerService。
//
// 职责（对齐 pitaya service.HandlerService + handlerPool）：
//  1. 解码：把二进制帧解成 protocol.Message（复用现有 protocol.Decode）
//  2. 定位：按 MsgID 找到注册的 handler
//  3. 前置：执行 pipeline Before 钩子（认证、限流）
//  4. 分发：用反射把 body 反序列化到 *Req，调用 func(ctx,*Req)(*Resp,error)
//  5. 后置：执行 pipeline After 钩子
//  6. 编码：把 *Resp 编码为响应帧（复用现有 protocol.Encode），或把错误编码为 MsgID_Error 帧
//
// 为什么用反射？
//
//	业务 handler 的 Req/Resp 类型各不相同，内核要以统一方式处理，
//	只能在运行期通过反射构造 Req、调用方法、取 Resp。pitaya 亦然。
//	handler 在启动期一次性注册，运行期只读，无需加锁；反射开销对
//	消息量很小的挂机类游戏可忽略。
package kernel

import (
	"context"
	"encoding/json"
	"reflect"

	"game-server/internal/pipeline"
	"game-server/internal/protocol"
	"game-server/internal/session"

	"go.uber.org/zap"
)

// handlerEntry 是一条注册记录：把请求 MsgID 映射到具体处理逻辑。
type handlerEntry struct {
	respID   uint16        // 正常响应帧使用的 MsgID
	reqType  reflect.Type  // 请求体的具体类型（指针指向的元素类型），nil 表示无请求体
	fn       reflect.Value // 业务方法：func(ctx, *Req) (*Resp, error)
	authFree bool          // 是否免鉴权（登录、心跳），对齐旧 Router 的 skip 逻辑
}

// Kernel 消息处理内核。
type Kernel struct {
	handlers map[uint16]*handlerEntry
	hooks    *pipeline.Hooks
}

// New 创建内核。hooks 可为 nil（表示无前置/后置钩子）。
func New(hooks *pipeline.Hooks) *Kernel {
	if hooks == nil {
		hooks = pipeline.New()
	}
	return &Kernel{
		handlers: make(map[uint16]*handlerEntry),
		hooks:    hooks,
	}
}

// RegisterOption 注册可选项。
type RegisterOption func(*handlerEntry)

// AuthFree 标记该消息免鉴权（登录、心跳）。
func AuthFree() RegisterOption {
	return func(e *handlerEntry) { e.authFree = true }
}

// 预先取得几个反射类型，用于校验 handler 签名。
var (
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errType = reflect.TypeOf((*error)(nil)).Elem()
)

// Register 注册一个业务 handler。
//
// 约束 fn 的签名必须是：func(context.Context, *Req) (*Resp, error)
//   - reqID：请求消息 ID
//   - respID：正常响应消息 ID（响应帧用它编码）
//   - fn：业务方法
//
// 签名不符会 panic（编程错误，应尽早在启动期暴露），对齐旧 Router.Register 的做法。
func (k *Kernel) Register(reqID, respID uint16, fn interface{}, opts ...RegisterOption) {
	if _, exists := k.handlers[reqID]; exists {
		zap.L().Panic("重复注册消息处理器", zap.Uint16("reqID", reqID))
	}

	fv := reflect.ValueOf(fn)
	ft := fv.Type()
	if ft.Kind() != reflect.Func {
		zap.L().Panic("注册的 handler 不是函数", zap.Uint16("reqID", reqID))
	}
	// 入参：ctx, *Req
	if ft.NumIn() != 2 || !ft.In(0).Implements(ctxType) || ft.In(1).Kind() != reflect.Ptr {
		zap.L().Panic("handler 入参必须为 (context.Context, *Req)", zap.Uint16("reqID", reqID))
	}
	// 出参：*Resp, error
	if ft.NumOut() != 2 || ft.Out(0).Kind() != reflect.Ptr || !ft.Out(1).Implements(errType) {
		zap.L().Panic("handler 出参必须为 (*Resp, error)", zap.Uint16("reqID", reqID))
	}

	entry := &handlerEntry{
		respID:  respID,
		reqType: ft.In(1).Elem(), // *Req -> Req
		fn:      fv,
	}
	for _, opt := range opts {
		opt(entry)
	}
	k.handlers[reqID] = entry
}

// IsAuthFree 返回某消息是否免鉴权（供 pipeline 钩子判断）。
func (k *Kernel) IsAuthFree(msgID uint16) bool {
	e, ok := k.handlers[msgID]
	return ok && e.authFree
}

// HasHandler reports whether a request message has a registered handler.
func (k *Kernel) HasHandler(msgID uint16) bool {
	_, ok := k.handlers[msgID]
	return ok
}

// Dispatch 处理一整帧数据：解码 -> 定位 -> 前置 -> 反射调用 -> 后置 -> 编码并发送响应。
//
// ctx 必须已携带 *session.Session（由 transport 层注入）。
// 所有错误都会被编码成响应帧回给客户端，Dispatch 本身不返回 error。
func (k *Kernel) Dispatch(ctx context.Context, data []byte) {
	sess := session.FromContext(ctx)

	msg, err := protocol.Decode(data)
	if err != nil {
		zap.L().Error("消息解码失败", zap.Error(err))
		k.sendError(sess, protocol.NewBizError(protocol.ErrInvalidParam, "消息格式错误"))
		return
	}

	entry, ok := k.handlers[msg.MsgID]
	if !ok {
		zap.L().Warn("未注册的消息ID", zap.Uint16("msgID", msg.MsgID))
		k.sendError(sess, protocol.NewBizError(protocol.ErrInvalidParam, "不支持的消息类型"))
		return
	}

	// 反射构造 *Req 并反序列化 body。
	reqPtr := reflect.New(entry.reqType) // *Req
	if len(msg.Body) > 0 {
		if err := json.Unmarshal(msg.Body, reqPtr.Interface()); err != nil {
			zap.L().Error("请求体反序列化失败", zap.Uint16("msgID", msg.MsgID), zap.Error(err))
			k.sendError(sess, protocol.NewBizError(protocol.ErrInvalidParam, "请求格式错误"))
			return
		}
	}

	// 前置钩子（认证、限流）。把 msgID 透传给钩子做 authFree 判断。
	ctx = withMsgID(ctx, msg.MsgID)
	ctx, _, err = k.hooks.ExecuteBefore(ctx, reqPtr.Interface())
	if err != nil {
		k.finish(sess, entry, nil, err)
		return
	}

	// 反射调用业务方法。
	out := entry.fn.Call([]reflect.Value{reflect.ValueOf(ctx), reqPtr})
	respVal := out[0]
	var handlerErr error
	if e, ok := out[1].Interface().(error); ok {
		handlerErr = e
	}

	// 后置钩子。
	var respIface interface{}
	if !respVal.IsNil() {
		respIface = respVal.Interface()
	}
	respIface, handlerErr = k.hooks.ExecuteAfter(ctx, respIface, handlerErr)

	k.finish(sess, entry, respIface, handlerErr)
}

// finish 根据 handler 结果编码并发送响应。
func (k *Kernel) finish(sess *session.Session, entry *handlerEntry, resp interface{}, err error) {
	if err != nil {
		k.sendError(sess, err)
		return
	}
	// resp 为 nil 表示 handler 不需要回响应（fire-and-forget）。
	if resp == nil {
		return
	}
	if sess == nil {
		return
	}
	if sendErr := sess.Push(entry.respID, resp); sendErr != nil {
		zap.L().Error("发送响应失败", zap.Uint16("respID", entry.respID), zap.Error(sendErr))
	}
}

// sendError 把错误编码为 MsgID_Error 帧发送。
// BizError 用其携带的 code/msg；其他 error 归为 ErrInternal，避免泄露内部细节。
func (k *Kernel) sendError(sess *session.Session, err error) {
	if sess == nil {
		return
	}
	code := protocol.ErrInternal
	msg := "服务器内部错误"
	if be, ok := err.(*protocol.BizError); ok {
		code = be.Code
		msg = be.Msg
	} else {
		zap.L().Error("handler 系统错误", zap.Error(err))
	}
	_ = sess.Push(protocol.MsgID_Error, protocol.ErrorResp{Code: code, Msg: msg})
}

// ---- ctx 携带 msgID，供 pipeline 钩子判断 authFree ----

type msgIDKey struct{}

func withMsgID(ctx context.Context, msgID uint16) context.Context {
	return context.WithValue(ctx, msgIDKey{}, msgID)
}

// MsgIDFromContext 从 ctx 取当前处理的消息 ID；不存在返回 0。
func MsgIDFromContext(ctx context.Context) uint16 {
	id, _ := ctx.Value(msgIDKey{}).(uint16)
	return id
}
