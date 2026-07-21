package kernel

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"game-server/internal/pipeline"
	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var ErrFatalProtocol = errors.New("fatal protocol violation")

type handlerEntry struct {
	respID   uint16
	reqType  reflect.Type
	fn       reflect.Value
	authFree bool
}

type Kernel struct {
	handlers map[uint16]*handlerEntry
	hooks    *pipeline.Hooks
}

func New(hooks *pipeline.Hooks) *Kernel {
	if hooks == nil {
		hooks = pipeline.New()
	}
	return &Kernel{handlers: make(map[uint16]*handlerEntry), hooks: hooks}
}

type RegisterOption func(*handlerEntry)

func AuthFree() RegisterOption { return func(e *handlerEntry) { e.authFree = true } }

var (
	ctxType          = reflect.TypeOf((*context.Context)(nil)).Elem()
	errType          = reflect.TypeOf((*error)(nil)).Elem()
	protoMessageType = reflect.TypeOf((*proto.Message)(nil)).Elem()
)

// Register fails closed at startup: only generated protobuf request and
// response pointers are valid route handlers.
func (k *Kernel) Register(reqID, respID uint16, fn interface{}, opts ...RegisterOption) {
	if _, exists := k.handlers[reqID]; exists {
		panic("duplicate message handler")
	}
	route, found := protocol.RouteFor(reqID)
	if !found || route.ResponseID != respID {
		panic("unknown or mismatched message route")
	}
	ft := reflect.TypeOf(fn)
	if ft == nil || ft.Kind() != reflect.Func || ft.NumIn() != 2 || ft.NumOut() != 2 ||
		!ft.In(0).Implements(ctxType) || ft.In(1).Kind() != reflect.Ptr || !ft.In(1).Implements(protoMessageType) ||
		ft.Out(0).Kind() != reflect.Ptr || !ft.Out(0).Implements(protoMessageType) || !ft.Out(1).Implements(errType) {
		panic("handler must be func(context.Context, *protocolpb.Request) (*protocolpb.Response, error)")
	}
	if ft.In(1) != reflect.TypeOf(route.RequestPrototype) || ft.Out(0) != reflect.TypeOf(route.ResponsePrototype) {
		panic("handler protobuf types do not match the canonical message route")
	}
	entry := &handlerEntry{respID: respID, reqType: ft.In(1).Elem(), fn: reflect.ValueOf(fn)}
	for _, opt := range opts {
		opt(entry)
	}
	k.handlers[reqID] = entry
}

func (k *Kernel) RegisterRoute(reqID uint16, fn interface{}, opts ...RegisterOption) {
	route, found := protocol.RouteFor(reqID)
	if !found {
		panic("unknown message route")
	}
	k.Register(reqID, route.ResponseID, fn, opts...)
}

func (k *Kernel) IsAuthFree(msgID uint16) bool {
	entry, ok := k.handlers[msgID]
	return ok && entry.authFree
}
func (k *Kernel) HasHandler(msgID uint16) bool { _, ok := k.handlers[msgID]; return ok }

func (k *Kernel) Dispatch(ctx context.Context, data []byte) error {
	sess := session.FromContext(ctx)
	frame, err := protocol.Decode(data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFatalProtocol, err)
	}
	if frame.Seq == 0 {
		return ErrFatalProtocol
	}
	entry, ok := k.handlers[frame.MsgID]
	if !ok {
		k.sendError(sess, frame.Seq, protocol.NewBizError(protocol.ErrInvalidParam, "unsupported message type"))
		return nil
	}
	req, ok := reflect.New(entry.reqType).Interface().(proto.Message)
	if !ok || proto.Unmarshal(frame.Body, req) != nil {
		k.sendError(sess, frame.Seq, protocol.NewBizError(protocol.ErrInvalidParam, "invalid request payload"))
		return nil
	}
	ctx = withMsgID(ctx, frame.MsgID)
	ctx, _, err = k.hooks.ExecuteBefore(ctx, req)
	if err != nil {
		k.finish(sess, frame.Seq, entry, nil, err)
		return nil
	}
	result := entry.fn.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)})
	var response interface{}
	if !result[0].IsNil() {
		response = result[0].Interface()
	}
	var handlerErr error
	if !result[1].IsNil() {
		handlerErr, _ = result[1].Interface().(error)
	}
	response, handlerErr = k.hooks.ExecuteAfter(ctx, response, handlerErr)
	k.finish(sess, frame.Seq, entry, response, handlerErr)
	return nil
}

func (k *Kernel) finish(sess *session.Session, seq uint32, entry *handlerEntry, response interface{}, err error) {
	if err != nil {
		k.sendError(sess, seq, err)
		return
	}
	if response == nil || sess == nil {
		return
	}
	message, ok := response.(proto.Message)
	if !ok {
		k.sendError(sess, seq, protocol.NewBizError(protocol.ErrInternal, "invalid handler response"))
		return
	}
	if err := sess.Reply(seq, entry.respID, message); err != nil {
		zap.L().Error("send websocket response", zap.Uint16("message_id", entry.respID), zap.Error(err))
	}
}

func (k *Kernel) sendError(sess *session.Session, seq uint32, err error) {
	if sess == nil {
		return
	}
	code, message := protocol.ErrInternal, "internal server error"
	if business, ok := err.(*protocol.BizError); ok {
		code, message = business.Code, business.Msg
	} else {
		zap.L().Error("kernel handler error", zap.Error(err))
	}
	if sendErr := sess.Reply(seq, protocol.MsgID_Error, &protocolpb.ErrorResp{Code: int32(code), Msg: message}); sendErr != nil {
		zap.L().Error("send protobuf error response", zap.Error(sendErr))
	}
}

type msgIDKey struct{}

func withMsgID(ctx context.Context, msgID uint16) context.Context {
	return context.WithValue(ctx, msgIDKey{}, msgID)
}
func WithMsgID(ctx context.Context, msgID uint16) context.Context { return withMsgID(ctx, msgID) }
func MsgIDFromContext(ctx context.Context) uint16                 { id, _ := ctx.Value(msgIDKey{}).(uint16); return id }
