@echo off
setlocal enabledelayedexpansion

title Hanxi Build and Package Tool

echo =======================================================
echo          Hanxi Build and Portable Package
echo =======================================================
echo.

:: 1. Navigate to repository root
cd /d "%~dp0.."

:: 2. Check environment
echo [1/4] Checking prerequisites...
where node >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Node.js not found. Please install Node.js first.
    goto :error
)

where go >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go not found. Please install Go 1.26+.
    goto :error
)

where task >nul 2>&1
if %errorlevel% neq 0 (
    echo [INFO] go-task not found, installing...
    go install github.com/go-task/task/v3/cmd/task@v3.53.1
)

where wails3 >nul 2>&1
if %errorlevel% neq 0 (
    echo [INFO] Wails v3 CLI not found, installing...
    go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.10
)

:: 3. Build frontend and backend
:: 注: 生产构建已通过 Taskfile 的 -ldflags="-w -s -H windowsgui" 去除符号表/DWARF 减体积。
::     不再使用 UPX 压缩 —— UPX 运行时需在内存中解压完整原始镜像且无法换出,
::     实测工作集比未压缩版本高出数倍 (9MB → 90MB+), 属纯亏。
echo [2/3] Building production binary...
if not exist "frontend\node_modules" (
    echo Installing frontend dependencies...
    cd frontend && npm install && cd ..
)

echo Executing task build...
call task build
if %errorlevel% neq 0 (
    echo [ERROR] Build failed. Please check compiler output.
    goto :error
)

if not exist "bin\hanxi.exe" (
    echo [ERROR] Output bin\hanxi.exe not found.
    goto :error
)

:: 4. Assemble portable distribution
echo.
echo [3/3] Assembling portable package...
if not exist "bin\portable" mkdir "bin\portable"
if not exist "bin\portable\data" mkdir "bin\portable\data"

copy /y "bin\hanxi.exe" "bin\portable\hanxi.exe" >nul
copy /y "README.md" "bin\portable\README.md" >nul

echo.
echo =======================================================
echo  [SUCCESS] Build and package completed!
echo =======================================================
echo  - Standalone Executable : bin\hanxi.exe
echo  - Portable Directory    : bin\portable\ (with data\ flag)
echo =======================================================
echo.
goto :end

:error
echo.
echo [FAILED] Build process failed.
echo.
exit /b 1

:end
exit /b 0