-- 游戏服务器数据库初始化脚本
-- Docker 首次启动 MySQL 时自动执行
-- 注意：GORM AutoMigrate 也会创建表，此脚本作为补充

-- 确保使用 utf8mb4 字符集（支持 emoji）
ALTER DATABASE game_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
