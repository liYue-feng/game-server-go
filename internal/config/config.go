// Package config 提供统一的配置加载能力，基于 spf13/viper 封装。
//
// 为什么用 Viper？
//   - 支持 YAML/JSON/TOML/环境变量等多种配置源
//   - 支持配置热更新（WatchConfig），未来可以不重启修改配置
//   - Go 生态中最流行的配置库
//
// 使用方式：
//
//	cfg, err := config.Load("configs/config.yaml")
//	addr := cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port)
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config 顶层配置结构，与 config.yaml 一一对应
// 每个子结构体对应 YAML 中的一个顶级键
type Config struct {
	Server      ServerConfig      `mapstructure:"server"` // 服务器配置
	Redis       RedisConfig       `mapstructure:"redis"`  // Redis 配置
	MySQL       MySQLConfig       `mapstructure:"mysql"`  // MySQL 配置
	Wechat      WechatConfig      `mapstructure:"wechat"` // 微信配置
	Log         LogConfig         `mapstructure:"log"`    // 日志配置
	GM          GMConfig          `mapstructure:"gm"`     // GM 指令配置
	Development DevelopmentConfig `mapstructure:"development"`
}

// DevelopmentConfig 控制仅供本地开发使用的基础设施和登录捷径。
type DevelopmentConfig struct {
	Enabled      bool `mapstructure:"enabled"`
	LoginEnabled bool `mapstructure:"login_enabled"`
}

// ServerConfig 服务器基础配置
type ServerConfig struct {
	Host string `mapstructure:"host"` // 监听地址
	Port int    `mapstructure:"port"` // 监听端口
	Name string `mapstructure:"name"` // 服务名称
}

// Addr 返回监听地址，格式 "host:port"
func (s *ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// GMConfig GM 指令配置
//
// 为什么把管理员白名单放配置里？
//   - 管理员 UID 属于运维数据，硬编码每次调整都要重新编译发布
//   - 放配置后可随环境区分，也可通过环境变量 GAME_GM_ADMIN_UIDS 注入
type GMConfig struct {
	AdminUIDs []int64 `mapstructure:"admin_uids"` // 管理员 UID 白名单
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	Host     string `mapstructure:"host"`     // Redis 地址
	Port     int    `mapstructure:"port"`     // Redis 端口
	Password string `mapstructure:"password"` // 密码，无密码留空
	DB       int    `mapstructure:"db"`       // 数据库编号 0-15
}

// Addr 返回 Redis 地址，格式 "host:port"
func (r *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

// MySQLConfig MySQL 连接配置
type MySQLConfig struct {
	Host         string `mapstructure:"host"`           // MySQL 地址
	Port         int    `mapstructure:"port"`           // MySQL 端口
	User         string `mapstructure:"user"`           // 用户名
	Password     string `mapstructure:"password"`       // 密码
	DBName       string `mapstructure:"dbname"`         // 数据库名
	MaxIdleConns int    `mapstructure:"max_idle_conns"` // 空闲连接池最大连接数
	MaxOpenConns int    `mapstructure:"max_open_conns"` // 最大打开连接数
}

// DSN 返回 MySQL 数据源名称（Data Source Name）
// 格式：user:password@tcp(host:port)/dbname?params
func (m *MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		m.User, m.Password, m.Host, m.Port, m.DBName)
}

// WechatConfig 微信小游戏配置
type WechatConfig struct {
	AppID     string `mapstructure:"app_id"`     // 小游戏 AppID
	AppSecret string `mapstructure:"app_secret"` // 小游戏 AppSecret

	// 微信支付配置
	MchID             string `mapstructure:"mch_id"`               // 微信支付商户号
	MchPrivateKeyPath string `mapstructure:"mch_private_key_path"` // 商户私钥文件路径（apiv3 要求）
	MchCertSerialNo   string `mapstructure:"mch_cert_serial_no"`   // 商户证书序列号
	WechatPayCertPath string `mapstructure:"wechat_pay_cert_path"` // 微信支付平台证书文件路径（用于验证回调签名）
	PayNotifyURL      string `mapstructure:"pay_notify_url"`       // 支付回调通知 URL
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string `mapstructure:"level"`       // 日志级别
	Filename   string `mapstructure:"filename"`    // 日志文件路径
	MaxSize    int    `mapstructure:"max_size"`    // 单个日志文件最大 MB
	MaxBackups int    `mapstructure:"max_backups"` // 保留旧日志文件数量
	MaxAge     int    `mapstructure:"max_age"`     // 保留旧日志文件天数
}

// Load 从指定路径加载配置文件
//
// 内部流程：
//  1. 设置配置文件路径和格式
//  2. 读取环境变量覆盖（同名环境变量优先级更高，适合容器化部署）
//  3. 自动绑定环境变量，如 GAME_SERVER_PORT 会覆盖 server.port
//  4. Unmarshal 到 Config 结构体
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// 设置配置文件
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// 环境变量覆盖：容器部署时通过环境变量注入敏感配置（密码等）
	// 环境变量前缀 GAME_ ，如 GAME_MYSQL_PASSWORD=xxx 覆盖 mysql.password
	v.SetEnvPrefix("GAME")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 反序列化到结构体
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	return &cfg, nil
}
