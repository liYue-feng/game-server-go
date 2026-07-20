package kernel

import (
	"bytes"
	"context"
	"testing"

	"game-server/internal/pipeline"
	"game-server/internal/protocol"
	"game-server/internal/session"
)

// captureConn 是 session.Conn 的测试替身：
// 它按真实 transport 的方式用 protocol.Encode 生成帧字节并记录下来，
// 这样我们就能断言"内核发出的响应帧字节"与预期一致。
type captureConn struct {
	frames [][]byte
}

func (c *captureConn) SendMessage(msgID uint16, payload interface{}) error {
	frame, err := protocol.Encode(msgID, payload)
	if err != nil {
		return err
	}
	c.frames = append(c.frames, frame)
	return nil
}

// newCtx 构造携带会话的 ctx（模拟 transport 层注入）。
func newCtx(conn session.Conn) (context.Context, *session.Session) {
	s := session.New(conn)
	return session.WithSession(context.Background(), s), s
}

// TestGoldenWireCompatibility 是协议金标准测试：
// 证明改造后内核发出的响应帧，与旧 protocol.Encode 生成的字节完全一致，
// 即 Unity 客户端无需任何改动。
func TestGoldenWireCompatibility(t *testing.T) {
	k := New(nil)
	// 回显 handler：把请求里的 code 原样放进响应的 nickname，验证 req 正确反序列化。
	k.Register(protocol.MsgID_LoginReq, protocol.MsgID_LoginResp,
		func(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
			return &protocol.LoginResp{Uid: 1001, Nickname: req.Code, Token: "tok"}, nil
		})

	conn := &captureConn{}
	ctx, _ := newCtx(conn)

	// 构造客户端请求帧（与 Unity 发送格式一致）。
	reqFrame, err := protocol.Encode(protocol.MsgID_LoginReq, protocol.LoginReq{Code: "abc"})
	if err != nil {
		t.Fatalf("构造请求帧失败: %v", err)
	}

	k.Dispatch(ctx, reqFrame)

	if len(conn.frames) != 1 {
		t.Fatalf("应发送 1 帧响应, 实际 %d", len(conn.frames))
	}
	// 预期响应帧：用旧 Encode 生成的金标准字节。
	want, _ := protocol.Encode(protocol.MsgID_LoginResp, protocol.LoginResp{Uid: 1001, Nickname: "abc", Token: "tok"})
	if !bytes.Equal(conn.frames[0], want) {
		t.Fatalf("响应帧字节不匹配\n got=%v\nwant=%v", conn.frames[0], want)
	}
}

// TestBizErrorEncoded 验证 handler 返回 BizError 时编码为 MsgID_Error 帧且携带正确 code。
func TestBizErrorEncoded(t *testing.T) {
	k := New(nil)
	k.Register(protocol.MsgID_LoginReq, protocol.MsgID_LoginResp,
		func(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
			return nil, protocol.NewBizError(protocol.ErrLoginWechatFailed, "微信登录失败")
		})

	conn := &captureConn{}
	ctx, _ := newCtx(conn)
	reqFrame, _ := protocol.Encode(protocol.MsgID_LoginReq, protocol.LoginReq{Code: "x"})
	k.Dispatch(ctx, reqFrame)

	want, _ := protocol.Encode(protocol.MsgID_Error, protocol.ErrorResp{Code: protocol.ErrLoginWechatFailed, Msg: "微信登录失败"})
	if len(conn.frames) != 1 || !bytes.Equal(conn.frames[0], want) {
		t.Fatalf("BizError 未正确编码为错误帧")
	}
}

// TestSystemErrorMasked 验证普通 error 被归一为 ErrInternal，不泄露内部信息。
func TestSystemErrorMasked(t *testing.T) {
	k := New(nil)
	k.Register(protocol.MsgID_LoginReq, protocol.MsgID_LoginResp,
		func(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
			return nil, context.DeadlineExceeded // 任意普通 error
		})
	conn := &captureConn{}
	ctx, _ := newCtx(conn)
	reqFrame, _ := protocol.Encode(protocol.MsgID_LoginReq, protocol.LoginReq{Code: "x"})
	k.Dispatch(ctx, reqFrame)

	want, _ := protocol.Encode(protocol.MsgID_Error, protocol.ErrorResp{Code: protocol.ErrInternal, Msg: "服务器内部错误"})
	if len(conn.frames) != 1 || !bytes.Equal(conn.frames[0], want) {
		t.Fatalf("系统 error 未归一为 ErrInternal")
	}
}

// TestUnknownMsgID 验证未注册消息返回参数无效错误。
func TestUnknownMsgID(t *testing.T) {
	k := New(nil)
	conn := &captureConn{}
	ctx, _ := newCtx(conn)
	// 用一个未注册的 MsgID 构帧。
	reqFrame, _ := protocol.Encode(uint16(59999), struct{}{})
	k.Dispatch(ctx, reqFrame)
	if len(conn.frames) != 1 {
		t.Fatalf("未注册消息应回一帧错误, 实际 %d", len(conn.frames))
	}
}

// TestBeforeHookBreak 验证前置钩子中断时，handler 不执行且返回错误帧。
func TestBeforeHookBreak(t *testing.T) {
	hooks := pipeline.New()
	hooks.AddBefore(func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		return ctx, in, protocol.NewBizError(protocol.ErrUnauthorized, "请先登录")
	})
	k := New(hooks)
	handlerCalled := false
	k.Register(protocol.MsgID_SaveArchiveReq, protocol.MsgID_SaveArchiveResp,
		func(ctx context.Context, req *protocol.SaveArchiveReq) (*protocol.SaveArchiveResp, error) {
			handlerCalled = true
			return &protocol.SaveArchiveResp{Success: true}, nil
		})
	conn := &captureConn{}
	ctx, _ := newCtx(conn)
	reqFrame, _ := protocol.Encode(protocol.MsgID_SaveArchiveReq, protocol.SaveArchiveReq{Data: "{}"})
	k.Dispatch(ctx, reqFrame)

	if handlerCalled {
		t.Fatal("前置钩子中断后 handler 不应执行")
	}
	want, _ := protocol.Encode(protocol.MsgID_Error, protocol.ErrorResp{Code: protocol.ErrUnauthorized, Msg: "请先登录"})
	if len(conn.frames) != 1 || !bytes.Equal(conn.frames[0], want) {
		t.Fatal("前置钩子错误未正确编码")
	}
}

// TestAuthFreeFlag 验证 AuthFree 注册项被正确标记。
func TestAuthFreeFlag(t *testing.T) {
	k := New(nil)
	k.Register(protocol.MsgID_LoginReq, protocol.MsgID_LoginResp,
		func(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
			return &protocol.LoginResp{}, nil
		},
		AuthFree())
	if !k.IsAuthFree(protocol.MsgID_LoginReq) {
		t.Fatal("MsgID_LoginReq 应为免鉴权")
	}
	if k.IsAuthFree(protocol.MsgID_SaveArchiveReq) {
		t.Fatal("未注册消息不应为免鉴权")
	}
}
