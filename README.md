# Go 游戏服务器

基于 Go 语言的微信小游戏后端服务器，面向"吸血鬼幸存者"类单机+社交游戏。

## 架构概览

```
┌─────────────┐     WebSocket      ┌──────────────────────────────────────┐
│  微信小游戏  │ ◄──────────────► │         游戏服务器 (Go)              │
│   客户端     │    :8080/ws      │                                      │
└─────────────┘                   │  ┌──────────┐  ┌──────────┐         │
                                  │  │  Gateway  │  │  Router   │         │
                                  │  │  网关层    │─►│  消息路由  │         │
                                  │  └──────────┘  └────┬─────┘         │
                                  │                     │               │
                                  │       ┌─────────────┼───────────┐   │
                                  │       ▼             ▼           ▼   │
                                  │  ┌─────────┐ ┌──────────┐ ┌──────┐ │
                                  │  │  Login  │ │  Game    │ │ Rank │ │
                                  │  │  登录   │ │  存档    │ │ 排行 │ │
                                  │  └────┬────┘ └────┬─────┘ └──┬───┘ │
                                  │       │           │          │     │
                                  │  ┌────▼───────────▼──────────▼──┐  │
                                  │  │         Store 数据层          │  │
                                  │  │   MySQL (持久化) + Redis     │  │
                                  │  └──────────────────────────────┘  │
                                  └──────────────────────────────────────┘
                                        ▲
                                        │ HTTP POST (支付回调)
                                  ┌─────┴──────┐
                                  │  微信支付   │
                                  │  服务器     │
                                  └────────────┘
```

## 功能模块

| 模块 | 说明 | 消息ID范围 |
|------|------|-----------|
| Login | 微信登录、心跳保活 | 1001-1099 |
| Game | 存档保存/加载 | 2001-2099 |
| Rank | 排行榜查询、分数提交 | 3001-3099 |
| Payment | 创建订单、支付回调 | 5001-5099 |
| GM | 管理员指令（踢人、广播、查询） | 6001-6099 |

## 通信协议

### 帧格式

```
+-------------------+-------------------+-------------------+
| Length (4 bytes)  | MsgID  (2 bytes)  | Body   (N bytes)  |
+-------------------+-------------------+-------------------+
  小端序 uint32       小端序 uint16        JSON 编码的消息体

Length = 6 + len(Body)
```

### 消息ID分配

```
1xxx  登录模块   1001=登录请求  1002=登录响应  1003=心跳请求  1004=心跳响应
2xxx  存档模块   2001=保存请求  2002=保存响应  2003=加载请求  2004=加载响应
3xxx  排行模块   3001=排行请求  3002=排行响应  3003=提交分数  3004=提交响应
5xxx  支付模块   5001=创建订单  5002=订单响应  5003=支付结果通知
6xxx  GM模块     6001=GM指令    6002=GM响应
9xxx  系统消息   9999=通用错误
```

### 示例：登录流程

```
客户端                                    服务器
  │                                         │
  │  ─── LoginReq {code: "wx_code"} ───►  │  1. 客户端用 wx.login() 获取 code
  │                                         │  2. 服务器用 code 换取 openid
  │  ◄── LoginResp {uid, token} ────────  │  3. 返回 uid + token
  │                                         │
  │  ─── HeartbeatReq {timestamp} ──────►  │  4. 每30秒发一次心跳
  │  ◄── HeartbeatResp {timestamp} ──────  │
  │                                         │
```

## 快速开始

### 环境要求

- Go 1.23+
- MySQL 8.0+
- Redis 7.0+

### 本地开发

```bash
# 1. 克隆项目
git clone https://github.com/liYue-feng/game-server-go.git
cd game_server_go

# 2. 初始化开发环境（激活 git hooks + 安装依赖）
make setup

# 3. 启动 MySQL 和 Redis（确保已安装）
# 或者用 Docker：docker-compose up mysql redis -d

# 4. 修改配置
# 编辑 configs/config.yaml，填入 MySQL/Redis 连接信息

# 5. 运行
make run

# 6. 编译
make build
```

> **跨设备开发提示**：`make setup` 会激活 git pre-commit hook，确保每次提交都包含 `.claude/` 配置文件。这样在其他电脑 clone 后，Claude Code 可以直接延续对话上下文。

### Docker 部署

```bash
# 一键启动（游戏服务器 + MySQL + Redis）
docker-compose up -d

# 查看日志
docker-compose logs -f game-server

# 停止
docker-compose down
```

### 常用命令

```bash
make build    # 编译
make run      # 运行
make test     # 测试
make fmt      # 格式化
make tidy     # 整理依赖
make clean    # 清理
```

## 项目结构

```
game_server_go/
├── cmd/server/main.go          # 入口程序
├── configs/config.yaml         # 配置文件
├── internal/
│   ├── config/                 # 配置加载 (Viper)
│   ├── protocol/               # 通信协议
│   │   ├── message.go          #   消息ID + 请求/响应结构体
│   │   ├── codec.go            #   二进制帧编解码
│   │   └── common.go           #   错误码定义
│   ├── gateway/                # 网关层
│   │   ├── server.go           #   WebSocket 服务器
│   │   ├── hub.go              #   连接管理中心
│   │   ├── connection.go       #   连接封装 + 读写泵
│   │   └── router.go           #   消息路由 + 中间件
│   ├── login/                  # 登录模块
│   │   ├── handler.go          #   登录 + 心跳处理
│   │   └── wechat.go           #   微信 API 客户端
│   ├── game/                   # 游戏模块
│   │   └── handler.go          #   存档保存/加载
│   ├── rank/                   # 排行榜模块
│   │   └── handler.go          #   排行查询 + 分数提交
│   ├── payment/                # 支付模块
│   │   ├── handler.go          #   订单创建 + 回调处理
│   │   └── constants.go        #   订单状态常量
│   ├── gm/                     # GM指令模块
│   │   └── handler.go          #   踢人/广播/查询
│   ├── middleware/              # 中间件
│   │   └── middleware.go       #   认证 + 限流
│   ├── model/                  # 数据模型 (GORM)
│   │   └── player.go           #   Player/Archive/ScoreRecord/PaymentOrder
│   └── store/                  # 数据存储层
│       ├── mysql.go            #   MySQL 操作
│       └── redis.go            #   Redis 操作（会话/排行/限流）
├── pkg/logger/                 # 日志封装 (zap + lumberjack)
│   └── logger.go
├── deploy/                     # 部署配置
│   └── mysql/init.sql          #   数据库初始化
├── Dockerfile                  # Docker 镜像
├── docker-compose.yml          # Docker Compose 编排
├── Makefile                    # 构建脚本
├── CLAUDE.md                   # AI 开发配置
└── AGENTS.md                   # 代码规范
```

## 配置说明

配置文件：`configs/config.yaml`

环境变量覆盖（`GAME_` 前缀）：

```bash
# 例：覆盖 MySQL 密码
GAME_MYSQL_PASSWORD=my_secret_password

# 例：覆盖 Redis 地址
GAME_REDIS_HOST=10.0.0.1
```

## 数据存储

### MySQL 表

| 表名 | 说明 |
|------|------|
| players | 玩家账号（openid、昵称、token、最高分） |
| archives | 游戏存档（JSON 格式，一个玩家一条） |
| score_records | 分数记录（每局一条，用于数据分析） |
| payment_orders | 支付订单（全生命周期：待支付→已支付→已发货） |

### Redis 用途

| Key 前缀 | 数据结构 | 说明 |
|-----------|---------|------|
| `session:{uid}` | Hash | 玩家会话缓存（2小时过期） |
| `rank:{type}` | Sorted Set | 排行榜（type=1最高分, type=2击杀数） |
| `rate:{key}` | String+TTL | 限流计数器 |

## 错误码

```
10001  服务器内部错误        20001  无效的微信登录code
10002  参数无效              20002  微信API调用失败
10003  请求过于频繁          20003  token已过期
10004  未授权               30001  存档保存失败
                             30002  存档不存在
                             40001  无效的排行榜类型
                             40002  无效的排名范围
```

## 开发路线

- [x] Phase 1: 项目骨架 — 目录结构、网关、消息路由、配置
- [x] Phase 2: 核心业务 — 登录、存档、排行榜、支付、GM
- [ ] Phase 3: 生产加固
  - [ ] 单元测试覆盖
  - [ ] 微信支付 V3 完整对接
  - [ ] 服务器优雅关闭（关闭所有 WebSocket 连接）
  - [ ] 配置热更新
  - [ ] Prometheus 监控指标
  - [ ] 压力测试

## 技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 网络框架 | gorilla/websocket | WebSocket 服务器 |
| 配置管理 | spf13/viper | YAML + 环境变量 |
| 日志 | uber-go/zap + lumberjack | 结构化日志 + 文件轮转 |
| 缓存 | go-redis/redis/v8 | 会话/排行榜/限流 |
| 数据库 | gorm.io/gorm + MySQL | ORM + 关系型存储 |
| 部署 | Docker + Docker Compose | 容器化部署 |
