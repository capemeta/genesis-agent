@echo off
chcp 65001 >nul
title Genesis Agent - TUI 对话

set GOCACHE=D:\gocache
set GOTMPDIR=D:\gotmp

echo.
echo  正在启动 Genesis Agent...
echo.

go run cmd/genesis-cli/main.go chat

echo.
echo  已退出。按任意键关闭窗口...
pause >nul
