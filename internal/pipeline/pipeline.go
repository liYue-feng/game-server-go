// Package pipeline 提供 handler 前置/后置钩子链，借鉴 pitaya 的 HandlerHooks。
//
// 为什么用 Pipeline 取代原来的 router.Use(MiddlewareFunc)？
//   - 原中间件签名 func(conn, body, next) 把"是否继续"耦合进每个中间件，
//     容易漏调 next 或重复发错误响应。
//   - Pipeline 把链式执行收敛到内核：Before 钩子返回 error 即中断，
//     业务无需关心 next；错误统一由内核编码为响应帧。
//   - 与 pitaya 的 BeforeHandler/AfterHandler 语义一致，便于对照学习。
//
// 执行时机（在 kernel 中）：
//
//	解码请求 -> ExecuteBefore(认证/限流) -> 反射调用 handler -> ExecuteAfter -> 编码响应
package pipeline

import "context"

// BeforeHandler 前置钩子：在 handler 执行前调用。
//
// 返回值：
//   - 新的 ctx（可注入信息，如已校验的 uid）
//   - 可能被改写的入参 in
//   - err != nil 时中断链，后续钩子与 handler 都不执行，err 由内核处理
type BeforeHandler func(ctx context.Context, in interface{}) (context.Context, interface{}, error)

// AfterHandler 后置钩子：在 handler 执行后调用，可观察/改写响应与错误。
type AfterHandler func(ctx context.Context, out interface{}, err error) (interface{}, error)

// Hooks 前置与后置钩子集合。启动期注册，运行期只读（无需加锁）。
type Hooks struct {
	Before []BeforeHandler
	After  []AfterHandler
}

// New 创建空钩子集合。
func New() *Hooks {
	return &Hooks{}
}

// AddBefore 追加前置钩子（按注册顺序执行）。
func (h *Hooks) AddBefore(fn BeforeHandler) {
	h.Before = append(h.Before, fn)
}

// AddAfter 追加后置钩子（按注册顺序执行）。
func (h *Hooks) AddAfter(fn AfterHandler) {
	h.After = append(h.After, fn)
}

// ExecuteBefore 依次执行前置钩子；任一钩子返回 error 即中断并把 error 上抛。
func (h *Hooks) ExecuteBefore(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
	res := in
	for _, fn := range h.Before {
		var err error
		ctx, res, err = fn(ctx, res)
		if err != nil {
			return ctx, res, err
		}
	}
	return ctx, res, nil
}

// ExecuteAfter 依次执行后置钩子，透传并可改写 out/err。
func (h *Hooks) ExecuteAfter(ctx context.Context, out interface{}, err error) (interface{}, error) {
	res := out
	for _, fn := range h.After {
		res, err = fn(ctx, res, err)
	}
	return res, err
}
