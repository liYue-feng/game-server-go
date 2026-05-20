.PHONY: build run clean test setup

# 项目名称
APP_NAME := game-server

# 构建输出目录
BUILD_DIR := bin

# Go 源码入口
MAIN_FILE := cmd/server/main.go

# 编译项目
build:
	@echo ">>> 编译 $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_FILE)
	@echo ">>> 编译完成: $(BUILD_DIR)/$(APP_NAME)"

# 直接运行（开发模式）
run:
	@echo ">>> 启动 $(APP_NAME)..."
	go run $(MAIN_FILE)

# 运行测试
test:
	@echo ">>> 运行测试..."
	go test -v ./...

# 清理编译产物
clean:
	@echo ">>> 清理..."
	rm -rf $(BUILD_DIR)
	rm -rf logs
	@echo ">>> 清理完成"

# 格式化代码
fmt:
	@echo ">>> 格式化代码..."
	go fmt ./...
	@echo ">>> 格式化完成"

# 代码静态检查（需要安装 golangci-lint）
lint:
	@echo ">>> 静态检查..."
	golangci-lint run ./...
	@echo ">>> 检查完成"

# 整理依赖
tidy:
	@echo ">>> 整理依赖..."
	go mod tidy
	@echo ">>> 整理完成"

# 初始化开发环境（clone 后首次运行）
setup:
	@echo ">>> 初始化开发环境..."
	git config core.hooksPath .git-hooks
	@echo ">>> Git hooks 已激活（每次提交自动包含 .claude/ 变更）"
	go mod tidy
	@echo ">>> 初始化完成"
