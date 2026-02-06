@echo off
title SFO Network Helper
echo ═══════════════════════════════════════════════════════════
echo            SFO NETWORK HELPER (Built-in Tools)
echo ═══════════════════════════════════════════════════════════
echo.
echo This tool helps fix hosting issues using Windows built-in
echo features only. No external apps required.
echo.
echo Running as: %USERNAME%
echo.

:: Check for admin rights
net session >nul 2>&1
if %errorLevel% == 0 (
    echo [OK] Running as Administrator
) else (
    echo [!] Not running as Administrator
    echo     Some features may not work. Right-click and "Run as Administrator"
)
echo.

echo ═══════════════════════════════════════════════════════════
echo OPTIONS:
echo.
echo   1. Show my IP address and hosting info
echo   2. Run full diagnostics
echo   3. Enable UPnP port forwarding
echo   4. Add Windows Firewall rule
echo   5. Start the game (Street Fighter Online.exe)
echo   6. Open PowerShell helper (advanced)
echo   7. Exit
echo.
echo ═══════════════════════════════════════════════════════════
echo.

:menu
set /p choice="Select option (1-7): "

if "%choice%"=="1" goto showip
if "%choice%"=="2" goto diagnose
if "%choice%"=="3" goto upnp
if "%choice%"=="4" goto firewall
if "%choice%"=="5" goto startgame
if "%choice%"=="6" goto powershell
if "%choice%"=="7" goto exit

echo Invalid choice.
goto menu

:showip
echo.
echo ═══════════════════════════════════════════════════════════
echo YOUR NETWORK INFO:
echo ═══════════════════════════════════════════════════════════
echo.
echo Local IP addresses:
for /f "tokens=2 delims=:" %%a in ('ipconfig ^| findstr /c:"IPv4"') do echo   %%a
echo.
echo Share this IP with your friend along with PORT: 1626
echo.
echo STEPS TO HOST:
echo   1. Start Street Fighter Online
echo   2. Go to Network Settings
echo   3. Turn Server ON
echo   4. Tell your friend your IP
echo.
pause
goto menu

:diagnose
echo.
echo Running diagnostics...
powershell -ExecutionPolicy Bypass -Command "& '%~dp0SFO_LocalRelay.ps1' -Mode diagnose"
goto menu

:upnp
echo.
echo Enabling UPnP port forwarding...
powershell -ExecutionPolicy Bypass -Command "& '%~dp0SFO_LocalRelay.ps1' -Mode upnp"
pause
goto menu

:firewall
echo.
echo Adding firewall rule for port 1626...
netsh advfirewall firewall add rule name="SFO Game Port 1626" dir=in action=allow protocol=TCP localport=1626
echo.
echo If you see "Ok.", the rule was added successfully.
pause
goto menu

:startgame
echo.
echo Starting Street Fighter Online...
start "" "%~dp0Street Fighter Online.exe"
echo Game launched!
pause
goto menu

:powershell
echo.
echo Opening PowerShell helper...
start powershell -ExecutionPolicy Bypass -File "%~dp0SFO_LocalRelay.ps1"
goto menu

:exit
echo.
echo Goodbye!
timeout /t 2 >nul
