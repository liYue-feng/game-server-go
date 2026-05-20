---
name: game-server-go-overview
description: Go游戏服务器项目概览 — 吸血鬼幸存者类微信小游戏后端
metadata: 
  node_type: memory
  type: project
  originSessionId: 81f3e9c8-5a8f-4fae-aad2-c10099b479ff
---

Go语言游戏服务器项目，面向"吸血鬼幸存者"类微信小游戏。

**架构决策**: 单体模块化（非微服务），按 internal/ 分层，未来可拆。理由：小团队/独立开发，微服务增加的复杂度远大于收益。

**技术选型**:
- WebSocket: gorilla/websocket（已归档但稳定）
- 配置: spf13/viper (YAML + 环境变量覆盖)
- 日志: uber-go/zap + lumberjack
- Redis: go-redis/redis/v8
- MySQL: gorm.io/gorm
- 协议: 二进制帧头(4B长度+2B消息ID) + JSON载荷

**关键认知**: 吸血鬼幸存者类游戏的逻辑主要跑在客户端，服务器负责登录、存档、排行榜、支付，不是实时对战服务器。

**已完成模块**:
- 网关层 (gateway): WebSocket服务器、Hub连接管理、消息路由+中间件
- 登录模块 (login): 微信code2session、心跳
- 游戏模块 (game): 存档保存/加载
- 排行榜模块 (rank): Redis Sorted Set
- 支付模块 (payment): 订单创建、回调处理（签名验证存根）
- GM指令模块 (gm): 踢人、广播、查询玩家、在线数
- 中间件 (middleware): 认证、限流
- Docker部署: Dockerfile + docker-compose.yml

**待完成**:
- 单元测试
- 微信支付V3完整对接
- 优雅关闭WebSocket连接
- 配置热更新
- Prometheus监控

**How to apply**: 所有实现决策以此为基础，不引入不必要的分布式组件，优先可运行再迭代。
