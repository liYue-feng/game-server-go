// Package protocol — 编解码器
//
// 负责将 Go 结构体编码为二进制网络帧，以及将二进制帧解码为 Go 结构体。
//
// 帧格式（小端序）：
//
//	+-------------------+-------------------+-------------------+
//	| Length (4 bytes)  | MsgID  (2 bytes)  | Body   (N bytes)  |
//	+-------------------+-------------------+-------------------+
//
// Length = 4 + 2 + len(Body) = 6 + len(Body)
// 最大帧长度：64KB（限制 Body 不超过约 64K，防止异常大帧占用内存）
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	// 帧头大小 = 4(Length) + 2(MsgID)
	HeaderSize = 6

	// 最大帧长度 64KB —— 吸血鬼幸存者类游戏的消息体很小，64KB 绰绰有余
	// 超过此长度的帧可能是攻击或异常，直接拒绝
	MaxFrameSize = 64 * 1024
)

var (
	ErrFrameTooLarge = errors.New("帧长度超过最大限制")
	ErrInvalidHeader = errors.New("无效的帧头")
)

// Message 服务器内部的消息表示
// 从网络读取的二进制帧会被解析为此结构，再分发给对应的 handler
type Message struct {
	MsgID uint16          // 消息ID
	Body  json.RawMessage // 消息体（延迟解析，handler 内再反序列化到具体类型）
}

// Encode 将 MsgID + Go 结构体 编码为二进制帧
//
// 使用方式：
//
//	frame, err := Encode(protocol.MsgID_LoginResp, loginResp)
//	conn.WriteMessage(websocket.BinaryMessage, frame)
func Encode(msgID uint16, payload interface{}) ([]byte, error) {
	// 1. 将载荷序列化为 JSON
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("JSON编码失败: %w", err)
	}

	// 2. 计算总长度
	totalLen := HeaderSize + len(body)

	// 3. 分配缓冲区，一次性写入帧头+载荷，避免多次内存分配
	buf := make([]byte, totalLen)

	// 4. 写入帧头（小端序）
	// 为什么用小端序？x86/ARM 都是小端架构，省去字节序转换
	binary.LittleEndian.PutUint32(buf[0:4], uint32(totalLen))
	binary.LittleEndian.PutUint16(buf[4:6], msgID)

	// 5. 写入载荷
	copy(buf[HeaderSize:], body)

	return buf, nil
}

// Decode 从二进制帧解析出 Message
//
// 参数 data 是去掉 WebSocket 帧后的完整应用层数据
// 返回的 Message.Body 是 json.RawMessage，handler 内再按具体类型解析
func Decode(data []byte) (*Message, error) {
	// 1. 检查最小长度
	if len(data) < HeaderSize {
		return nil, ErrInvalidHeader
	}

	// 2. 读取帧头
	totalLen := binary.LittleEndian.Uint32(data[0:4])
	msgID := binary.LittleEndian.Uint16(data[4:6])

	// 3. 校验长度一致性
	if int(totalLen) != len(data) {
		return nil, fmt.Errorf("帧长度不匹配: 声明=%d 实际=%d", totalLen, len(data))
	}

	// 4. 安全检查：防止超大帧
	if totalLen > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}

	// 5. 提取消息体
	var body json.RawMessage
	if totalLen > HeaderSize {
		body = make(json.RawMessage, totalLen-HeaderSize)
		copy(body, data[HeaderSize:totalLen])
	}

	return &Message{
		MsgID: msgID,
		Body:  body,
	}, nil
}
