package payment

import (
	"google.golang.org/protobuf/proto"
)

type UIDPusher interface {
	PushToUID(uid int64, msgID uint16, payload proto.Message) bool
}
