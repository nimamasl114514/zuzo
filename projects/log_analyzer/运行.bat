@echo off
chcp 65001 >nul
echo ========================================
echo   zuzo 并发日志分析系统 - 一键运行
echo ========================================
echo.

echo [步骤 1] 生成日志文件
echo.
cd /d "%~dp0"
..\..\zuzo.exe 生成日志.hy
if errorlevel 1 goto :error

echo.
echo [步骤 2] 并发分析日志
echo.
..\..\zuzo.exe 分析日志.hy
if errorlevel 1 goto :error

echo.
echo [步骤 3] 词频统计
echo.
..\..\zuzo.exe 词频统计.hy
if errorlevel 1 goto :error

echo.
echo ========================================
echo   全部完成！
echo ========================================
goto :end

:error
echo.
echo [错误] 运行失败

:end
pause
