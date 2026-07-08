@echo off
chcp 65001 >nul
title Genesis Agent - TUI 对话

set GOCACHE=%~dp0.gocache
set GOMODCACHE=%~dp0.gomodcache
set GOTMPDIR=%~dp0.gotmp

rem Optional local secrets for quick testing. This file is gitignored.
rem Example in start.local.bat: set TAVILY_API_KEY=tvly-...
if exist "%~dp0start.local.bat" call "%~dp0start.local.bat"

echo.
if "%~1"=="" (
  echo  正在启动 Genesis Agent...
  echo.
  go run cmd/genesis-cli/main.go chat
) else (
  echo  正在执行 Genesis CLI: %*
  echo.
  go run cmd/genesis-cli/main.go %*
)
echo.
echo  已退出。按任意键关闭窗口...
pause >nul
