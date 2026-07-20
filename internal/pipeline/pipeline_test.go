package pipeline

import (
	"context"
	"errors"
	"testing"
)

// TestBeforeChainOrder 验证 Before 钩子按注册顺序执行且可透传/改写入参。
func TestBeforeChainOrder(t *testing.T) {
	h := New()
	var order []int
	h.AddBefore(func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		order = append(order, 1)
		return ctx, in, nil
	})
	h.AddBefore(func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		order = append(order, 2)
		return ctx, "changed", nil
	})
	_, out, err := h.ExecuteBefore(context.Background(), "orig")
	if err != nil {
		t.Fatalf("意外 error: %v", err)
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("执行顺序错误: %v", order)
	}
	if out != "changed" {
		t.Fatalf("入参改写未生效: %v", out)
	}
}

// TestBeforeChainBreak 验证某钩子返回 error 时中断，后续钩子不执行。
func TestBeforeChainBreak(t *testing.T) {
	h := New()
	sentinel := errors.New("拒绝")
	reached := false
	h.AddBefore(func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		return ctx, in, sentinel
	})
	h.AddBefore(func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		reached = true
		return ctx, in, nil
	})
	_, _, err := h.ExecuteBefore(context.Background(), nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("应返回中断 error, got %v", err)
	}
	if reached {
		t.Fatal("中断后不应执行后续钩子")
	}
}

// TestAfterChain 验证 After 钩子按顺序执行且可改写响应。
func TestAfterChain(t *testing.T) {
	h := New()
	h.AddAfter(func(ctx context.Context, out interface{}, err error) (interface{}, error) {
		return out.(int) + 1, err
	})
	h.AddAfter(func(ctx context.Context, out interface{}, err error) (interface{}, error) {
		return out.(int) * 2, err
	})
	out, err := h.ExecuteAfter(context.Background(), 10, nil)
	if err != nil {
		t.Fatalf("意外 error: %v", err)
	}
	// (10+1)*2 = 22
	if out != 22 {
		t.Fatalf("After 改写结果错误: %v", out)
	}
}

// TestEmptyHooks 验证空钩子链原样透传。
func TestEmptyHooks(t *testing.T) {
	h := New()
	ctx, out, err := h.ExecuteBefore(context.Background(), "x")
	if err != nil || out != "x" || ctx == nil {
		t.Fatalf("空 Before 应原样透传: out=%v err=%v", out, err)
	}
	out2, err2 := h.ExecuteAfter(context.Background(), "y", nil)
	if err2 != nil || out2 != "y" {
		t.Fatalf("空 After 应原样透传: out=%v err=%v", out2, err2)
	}
}
