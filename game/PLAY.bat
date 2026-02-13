@echo off
:: Self-elevate for port 80 (HTTP firewall check) and hosts file access
net session >nul 2>&1
if %errorlevel% neq 0 (
    powershell.exe -Command "Start-Process cmd -ArgumentList '/c cd /d \"%~dp0\" && \"%~f0\"' -Verb RunAs"
    exit /b
)
cd /d "%~dp0"

echo ============================================
echo   Street Fighter Online - Launcher
echo   %date% %time%
echo ============================================
echo.
echo   1. Play Online (normal, direct to server)
echo   2. Play LAN   (NAT hairpin proxy + IP rewrite)
echo.
set /p choice="Choose (1 or 2): "

if "%choice%"=="2" goto LAN
goto ONLINE

:ONLINE
echo.
echo Starting game (online mode)...
start "" "Street Fighter Online.exe"
echo Game launched.
timeout /t 3 >nul
exit /b

:LAN
echo.
echo Starting LAN proxy (requires Node.js)...
where node >nul 2>&1
if %errorlevel% neq 0 (
    echo ERROR: Node.js not found. Install from https://nodejs.org
    pause
    exit /b
)
echo Proxy will rewrite server IPs for LAN connectivity.
start "SFO Proxy" /min node sfo_ip_proxy.mjs
timeout /t 2 >nul
echo Starting game...
start "" "Street Fighter Online.exe"
echo.
echo Game + proxy running.
echo Press any key when done playing to stop the proxy.
pause >nul
taskkill /f /fi "WINDOWTITLE eq SFO Proxy" >nul 2>&1
echo Proxy stopped. Goodbye.
timeout /t 2 >nul
exit /b
