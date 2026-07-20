@echo off
setlocal
title Game Server Development

set "APP_DIR=%~dp0"
cd /d "%APP_DIR%"

where go >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Go was not found on PATH.
    exit /b 1
)

if not exist "logs" mkdir "logs"

echo [INFO] Starting the development server on ws://127.0.0.1:8080/ws
echo [INFO] Press Ctrl+C to stop.
go run .\cmd\server -config .\configs\config.dev.yaml
set "EXIT_CODE=%ERRORLEVEL%"

if not "%EXIT_CODE%"=="0" echo [ERROR] Server exited with code %EXIT_CODE%.
exit /b %EXIT_CODE%
