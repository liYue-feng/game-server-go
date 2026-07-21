# Go 游戏服务器

基于 Go 语言的微信小游戏后端服务器，面向"吸血鬼幸存者"类单机+社交游戏。

## 架构概览

```
┌─────────────┐     WebSocket      ┌──────────────────────────────────────┐
│  微信小游戏  │ ◄──────────────► │         游戏服务器 (Go)              │
│   客户端     │    :8080/ws      │                                      │
└─────────────┘                   │  ┌──────────┐  ┌──────────┐         │
                                  │  │ Transport │  │  Kernel   │         │
                                  │  │ 传输+会话  │─►│ 内核+管道  │         │
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
```

## 功能模块

| 模块 | 说明 | 消息ID范围 |
|------|------|-----------|
| Login | 微信登录、心跳保活 | 1001-1099 |
| Game | 存档保存/加载 | 2001-2099 |
| Rank | 排行榜查询、分数提交 | 3001-3099 |
| Combat | 战斗结算、敌人/副本/流派配置、玩家属性 | 4001-4099 |
| Payment | 协议 ID 保留，生产支付禁用 | 5001-5099 |
| GM | 管理员指令（踢人、广播、查询） | 6001-6099 |

## 通信协议

### 帧格式

```
+-------------------+-------------------+-------------------+-------------------+
| Length (4 bytes)  | MsgID  (2 bytes)  | Seq    (4 bytes)  | Body   (N bytes)  |
+-------------------+-------------------+-------------------+-------------------+
  小端序 uint32       小端序 uint16        小端序 uint32        protobuf 编码的消息体

Length = 10 + len(Body)
```

Transport contract: 10-byte little-endian [Length uint32][MsgID uint16][Seq uint32]; Length includes the 10-byte header; request seq is nonzero; responses and errors echo the exact request seq; pushes use seq 0; Body is protobuf binary.

### 消息ID分配

```
1xxx  登录模块   1001=登录请求  1002=登录响应  1003=心跳请求  1004=心跳响应
2xxx  存档模块   2001=保存请求  2002=保存响应  2003=加载请求  2004=加载响应
3xxx  排行模块   3001=排行请求  3002=排行响应  3003=提交分数  3004=提交响应
4xxx  战斗模块   4001=战斗结算请求  4002=战斗结算响应  4003-4014=配置/流派/属性
5xxx  支付保留   5001=创建订单  5002=订单响应  5003=支付结果通知（生产禁用）
6xxx  GM模块     6001=GM指令    6002=GM响应
9xxx  系统消息   9999=通用错误
```

Payment protocol IDs 5001-5003 are reserved; production payment is disabled. `CreateOrderReq` 当前通过关联错误响应返回 `60001 payment is disabled`，服务端不接受支付回调，也不监听端口 `8081`。

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

### protobuf 协议生成与校验

`proto/game.proto` 是 32 个线上消息 ID 的共享协议定义。后端仅在本仓生成 `internal/protocolpb/game.pb.go`，Unity 客户端在自己的仓库生成 C# 代码。

```powershell
# 在后端仓库生成本地 Go 代码；ClientRoot 仅用于校验配套 Unity schema
powershell.exe -NoProfile -File tools/protobuf/Generate-Protocol.ps1

# 校验 schema、固定工具版本和后端 Go 已提交产物没有漂移
powershell.exe -NoProfile -File tools/protobuf/Verify-Protocol.ps1 -ClientRoot ..\game-client-unity
```

服务端本地生成器仅固定并校验 `protoc 35.0` 与 `protoc-gen-go v1.36.11`，且只写 Go 产物。`Google.Protobuf 3.35.1` 及其 NuGet 输入校验属于 Unity 客户端本地生成器和跨仓工具链职责；服务端生成器不下载或校验该包。

### 内存开发服与真实客户端联调

`configs/config.dev.yaml` 启用隔离的内存开发运行时和 `dev:` 登录，不依赖 MySQL、Redis 或微信登录服务：

```powershell
go run ./cmd/server -config configs/config.dev.yaml
go run ./cmd/devprobe
```

配套 Unity 仓库提供一次性真实后端 runner。它先运行 Go 测试和 `devprobe`，再执行 3 个 Unity PlayMode 用例：protobuf 存档往返、胜利结算持久化、失败结算持久化。

```powershell
# 在 game-client-unity 仓库执行
powershell.exe -NoProfile -File tools/integration/Invoke-A4BackendIntegration.ps1 -BackendRoot ..\game-server-go
```

## 项目结构

```
game_server_go/
├── cmd/server/main.go          # 入口程序（生产/内存开发运行时）
├── cmd/devprobe/main.go        # protobuf 开发服真实连接探针
├── configs/config.yaml         # 配置文件
├── proto/game.proto             # 与 Unity 仓库字节一致的共享 schema
├── internal/
│   ├── config/                 # 配置加载 (Viper)
│   ├── protocol/               # 通信协议
│   │   ├── ids.go              #   32 个消息 ID
│   │   ├── message.go          #   generated 类型兼容别名
│   │   ├── codec.go            #   二进制帧编解码
│   │   ├── routes.go           #   canonical 请求/响应路由表
│   │   ├── common.go           #   错误码定义
│   │   └── errors.go           #   BizError 业务错误
│   ├── protocolpb/             # protoc 生成的 Go 消息类型
│   ├── session/                # 玩家会话（Bind/UID/Set/Get/Push）
│   │   └── session.go          #   会话 + ctx 存取
│   ├── pipeline/               # 处理管道（Before/After 钩子）
│   │   └── pipeline.go         #   Hooks + ExecuteBefore/After
│   ├── kernel/                 # 消息内核（注册/反射分发）
│   │   └── kernel.go           #   Register/Dispatch + AuthFree
│   ├── transport/              # 传输层（WebSocket 收发）
│   │   ├── server.go           #   /ws + /health + 优雅关闭
│   │   ├── hub.go              #   连接管理中心
│   │   └── connection.go       #   连接封装 + 读写泵
│   ├── hooks/                  # pipeline 钩子
│   │   └── hooks.go            #   认证 + 限流
│   ├── login/                  # 登录模块
│   │   ├── handler.go          #   登录 + 心跳处理
│   │   └── wechat.go           #   微信 API 客户端
│   ├── game/                   # 游戏模块
│   │   └── handler.go          #   存档保存/加载
│   ├── combat/                 # 战斗结算、配置和玩家属性
│   │   ├── handler.go          #   4001-4014 消息处理
│   │   └── service.go          #   run_id 结算服务
│   ├── rank/                   # 排行榜模块
│   │   └── handler.go          #   排行查询 + 分数提交
│   ├── payment/                # 禁用的支付协议边界
│   │   └── handler.go          #   创建订单请求返回稳定业务错误
│   ├── gm/                     # GM指令模块
│   │   └── handler.go          #   踢人/广播/查询
│   ├── model/                  # 数据模型 (GORM)
│   │   ├── player.go           #   Player/Archive/ScoreRecord/PaymentOrder
│   │   └── combat_settlement.go #  每局结算幂等记录
│   └── store/                  # 数据存储层
│       ├── mysql.go            #   MySQL 操作与 AutoMigrate
│       ├── mysql_settlement.go #   MySQL 原子战斗结算
│       ├── settlement.go       #   结算奖励与存档更新规则
│       └── redis.go            #   Redis 操作（会话/排行/限流）
├── tools/protobuf/             # 固定版本协议生成与漂移校验
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
| archives | 游戏存档（`PlayerArchive` protobuf 字节，一个玩家一条） |
| score_records | 分数记录（每局一条，用于数据分析） |
| payment_orders | 休眠的历史支付订单模型；当前不创建、不更新、不发货 |
| combat_settlements | 战斗结算响应快照，唯一键 `(player_id, run_id)` 防止重复发奖 |

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
- [x] Phase 2: 核心业务 — 登录、存档、排行榜、GM，以及保留但禁用的支付协议边界
- [x] Phase 2.5: 参考 pitaya 重构网络层 — kernel（注册/反射分发）+ session（玩家会话）+ pipeline（Before/After 钩子）+ transport（WebSocket 收发），线上协议帧不变、客户端零改动
- [ ] Phase 3: 生产加固
  - [ ] 单元测试覆盖
  - [ ] 微信支付 V3 完整对接
  - [x] 服务器优雅关闭（关闭所有 WebSocket 连接）
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
| 消息序列化 | google.golang.org/protobuf | Go/Unity 共用 protobuf schema |
| 部署 | Docker + Docker Compose | 容器化部署 |
