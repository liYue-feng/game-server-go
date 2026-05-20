# Claude Code 配置：Go 游戏服务器

## 项目概述

Go 语言游戏服务器，面向"吸血鬼幸存者"类微信小游戏。
单体模块化架构，按 `internal/` 分层，未来可拆为微服务。

## 技术栈

- **语言**: Go 1.23+
- **网络**: gorilla/websocket
- **配置**: spf13/viper (YAML)
- **日志**: uber-go/zap + lumberjack
- **缓存**: go-redis/redis/v8
- **数据库**: gorm.io/gorm + MySQL
- **协议**: 二进制帧头(4B长度+2B消息ID) + JSON载荷

## 核心原则

1. **流程归 superpowers**：plan、brainstorm、debug、TDD、verify、code review，默认走 superpowers
2. **执行归 gstack**：浏览器、QA、ship、deploy、canary、retro 走 gstack
3. **独立 reviewer 通道**：verification 和 code-review 分两个 pass，不能在同一上下文里合并
4. **证据优先**：没有测试/截图/QA 报告不算完成
5. **歧义先 brainstorm**：任何创造性工作前先调用 brainstorming
6. **最短路径优先**：能用一个 skill 解决的，不升级为完整闭环

## 项目结构

```
game_server_go/
├── cmd/server/          # 入口程序
├── configs/             # 配置文件 (YAML)
├── internal/
│   ├── config/          # 配置加载 (Viper)
│   ├── gateway/         # 网关层：WebSocket 连接管理、消息路由
│   ├── login/           # 登录模块：微信登录、心跳
│   ├── game/            # 游戏模块：存档保存/加载
│   ├── rank/            # 排行榜模块：Redis Sorted Set
│   ├── model/           # 数据模型（与数据库表对应）
│   ├── store/           # 数据存储层（MySQL + Redis）
│   └── protocol/        # 通信协议：消息ID、编解码、错误码
├── pkg/logger/          # 日志封装 (zap + lumberjack)
└── Makefile
```

## 协议格式

```
+-------------------+-------------------+-------------------+
| Length (4 bytes)  | MsgID  (2 bytes)  | Body   (N bytes)  |
+-------------------+-------------------+-------------------+
小端序 uint32        小端序 uint16        JSON 编码的消息体
```

消息ID分段：1xxx=登录, 2xxx=存档, 3xxx=排行榜, 9xxx=系统

## 编码规范

- 遵循 Go 标准项目布局
- 注释写好 WHY，新手需要参考学习
- 错误处理：业务错误用 protocol.ErrorResp 返回，系统错误用 zap 记录
- 并发安全：WebSocket 连接的写操作集中在 writePump goroutine
- 数据库操作通过 store 层统一封装，业务层不直接操作 DB

## 任务分流

### 只读任务
分析、解释、架构说明、代码阅读 —— 直接处理。

### 轻量任务
单文件修改、明确 bug 修复、配置调整。
跳过完整流程，直接实现 + 定向验证。

### 中任务
多文件但边界清晰，新功能或明确重构。
简短 brainstorming + 短 plan + 实现 + verification。

### 大任务
跨模块、共享逻辑、新架构、公共 API 变更。
完整闭环：brainstorming → writing-plans → executing-plans + TDD → verification → code-review

## Subagent 策略

- **一定派**：用户明确要求并行、2-4 个边界清晰独立子任务、纯只读多目标研究
- **一定不派**：有顺序依赖、改同一文件、根因未明的调试、单一 bug 修复

## 安全护栏

- rm -rf / DROP TABLE / force-push 等危险命令必须先过 /careful
- 密钥不得硬编码，用环境变量覆盖（GAME_ 前缀）
- 数据库用参数化查询（GORM 默认参数化）

## Change Delivery Gate

声明完成前必须满足：
1. 已完成相关验证并如实报告
2. `go build ./...` 编译通过
3. 关键验证无法执行时明确说明原因
4. 禁止虚构命令输出
5. 没有验证证据，不得声称完成
