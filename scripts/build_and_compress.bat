@echo off
setlocal enabledelayedexpansion

title HubKit Build and UPX Package Tool

echo =======================================================
echo          HubKit Build and UPX Compression
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
    echo [ERROR] Go not found. Please install Go 1.24+.
    goto :error
)

where task >nul 2>&1
if %errorlevel% neq 0 (
    echo [INFO] go-task not found, installing...
    go install github.com/go-task/task/v3/cmd/task@latest
)

where wails3 >nul 2>&1
if %errorlevel% neq 0 (
    echo [INFO] Wails v3 CLI not found, installing...
    go install github.com/wailsapp/wails/v3/cmd/wails3@latest
)

:: 3. Build frontend and backend
echo [2/4] Building production binary...
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

if not exist "bin\hubkit.exe" (
    echo [ERROR] Output bin\hubkit.exe not found.
    goto :error
)

:: 4. UPX compression
echo.
echo [3/4] Running UPX compression (--best)...
set "UPX_BIN=build\tools\upx.exe"
if not exist "%UPX_BIN%" (
    where upx >nul 2>&1
    if %errorlevel% equ 0 (
        set "UPX_BIN=upx"
    ) else (
        echo [WARN] build\tools\upx.exe not found, skipping UPX compression.
        goto :package
    )
)

"%UPX_BIN%" --best bin\hubkit.exe

:package
:: 5. Assemble portable distribution
echo.
echo [4/4] Assembling portable package...
if not exist "bin\portable" mkdir "bin\portable"
if not exist "bin\portable\data" mkdir "bin\portable\data"

copy /y "bin\hubkit.exe" "bin\portable\hubkit.exe" >nul
copy /y "README.md" "bin\portable\README.md" >nul

echo.
echo =======================================================
echo  [SUCCESS] Build and UPX compression completed!
echo =======================================================
echo  - Standalone Executable : bin\hubkit.exe
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