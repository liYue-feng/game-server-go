// Package login — 微信小游戏 API 客户端
//
// 封装与微信服务器的 HTTP 交互，目前只实现登录凭证校验。
// 后续可扩展：支付回调、内容安全检测等。
//
// 微信登录流程（官方文档）：
//  1. 小游戏前端调用 wx.login() 获取临时登录凭证 code
//  2. 前端将 code 发送给我们的服务器
//  3. 我们的服务器调用微信 code2session API：
//     GET https://api.weixin.qq.com/sns/jscode2session
//       ?appid=APPID
//       &secret=APP_SECRET
//       &js_code=CODE
//       &grant_type=authorization_code
//  4. 微信返回 openid（用户唯一标识）和 session_key（会话密钥）
//  5. 我们用 openid 标识用户，session_key 用于数据解密（暂不使用）
package login

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"game-server/internal/config"

	"go.uber.org/zap"
)

// WechatClient 微信 API 客户端
type WechatClient struct {
	appID      string
	appSecret  string
	httpClient *http.Client // 复用 HTTP 连接，避免每次请求都建 TCP 连接
}

// NewWechatClient 创建微信 API 客户端
func NewWechatClient(cfg *config.WechatConfig) *WechatClient {
	return &WechatClient{
		appID:     cfg.AppID,
		appSecret: cfg.AppSecret,
		// 超时设置很重要：微信 API 不应该阻塞太久
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // 10秒超时，足够微信 API 响应
		},
	}
}

// Code2SessionResult 微信 code2session API 的返回值
type Code2SessionResult struct {
	OpenID     string `json:"openid"`      // 用户唯一标识
	SessionKey string `json:"session_key"` // 会话密钥（用于解密用户数据）
	UnionID    string `json:"unionid"`     // 开放平台唯一标识（需满足绑定条件）
	ErrCode    int    `json:"errcode"`     // 错误码
	ErrMsg     string `json:"errmsg"`      // 错误信息
}

// Code2Session 用微信登录凭证 code 换取 openid 和 session_key
//
// 这是微信小游戏登录的核心步骤。
// code 的有效期只有 5 分钟，且只能使用一次，所以必须及时调用。
func (c *WechatClient) Code2Session(code string) (*Code2SessionResult, error) {
	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		c.appID, c.appSecret, code,
	)

	// 发送 GET 请求
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求微信 API 失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取微信响应失败: %w", err)
	}

	zap.L().Debug("微信 code2session 响应", zap.String("body", string(body)))

	// 解析 JSON
	var result Code2SessionResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析微信响应失败: %w", err)
	}

	// 检查微信返回的错误码
	// 常见错误：
	//   - 40029: code 无效（过期或已使用）
	//   - 45011: 频率限制（每分钟 100 次）
	//   - -1: 系统繁忙
	if result.ErrCode != 0 {
		return nil, wechatAPIError(result)
	}

	return &result, nil
}

func wechatAPIError(result Code2SessionResult) error {
	if result.ErrCode == 40029 {
		return fmt.Errorf("%w: wechat API code=%d msg=%s", ErrInvalidLoginCode, result.ErrCode, result.ErrMsg)
	}
	return fmt.Errorf("wechat API error: code=%d msg=%s", result.ErrCode, result.ErrMsg)
}
