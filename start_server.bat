@echo off
chcp 936 >nul
title Game Server

set "APP_DIR=%~dp0"
cd /d "%APP_DIR%"

echo ============================================
echo   Game Server - Launch Script
echo ============================================
echo.

:: Check Go environment
where go >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go not found. Please install Go 1.23+: https://go.dev/dl/
    pause
    exit /b 1
)

:: Create logs directory
if not exist "logs" mkdir logs

:: Tidy dependencies if first launch
if not exist "go.sum" (
    echo [INFO] First launch, tidying dependencies...
    call go mod tidy
)

:: Start server
echo [INFO] Starting server (listening on :8080)...
echo [INFO] Press Ctrl+C to stop.
echo.
go run .\cmd\server\main.go

:: Pause on abnormal exit
if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Server exited abnormally, code: %errorlevel%
    pause
)