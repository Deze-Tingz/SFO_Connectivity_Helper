@echo off
title SFO Hosting Fix Test
color 0A
echo.
echo ═══════════════════════════════════════════════════════════
echo         SFO HOSTING FIX TEST (v4.0.0)
echo ═══════════════════════════════════════════════════════════
echo.

:: Get IP
for /f "tokens=2 delims=:" %%a in ('ipconfig ^| findstr /c:"IPv4"') do (
    set MYIP=%%a
    goto :gotip
)
:gotip
set MYIP=%MYIP: =%

echo Your IP: %MYIP%
echo.
echo ═══════════════════════════════════════════════════════════
echo TEST SETUP - You need TWO computers on the same network
echo ═══════════════════════════════════════════════════════════
echo.
echo   PC1 (HOST):    Runs the game + hosts
echo   PC2 (JOINER):  Runs the game + joins
echo.
echo ═══════════════════════════════════════════════════════════
echo WHICH PC IS THIS?
echo.
echo   1. This is PC1 - I want to HOST
echo   2. This is PC2 - I want to JOIN
echo   3. Run network diagnostics first
echo   4. Exit
echo.
echo ═══════════════════════════════════════════════════════════
echo.

:menu
set /p choice="Select (1-4): "

if "%choice%"=="1" goto host_test
if "%choice%"=="2" goto join_test
if "%choice%"=="3" goto diagnose
if "%choice%"=="4" exit

echo Invalid choice.
goto menu

:host_test
cls
echo.
echo ═══════════════════════════════════════════════════════════
echo         PC1: HOST TEST
echo ═══════════════════════════════════════════════════════════
echo.
echo STEP 1: Starting SFO Connectivity Helper as HOST...
echo.
echo   When the helper starts:
echo   - Select option 1 (HOST P2P) or option 3 (HOST Relay)
echo   - Note the JOIN CODE shown
echo   - Note your IP: %MYIP%
echo.
echo STEP 2: The game will launch automatically
echo.
echo STEP 3: In-game:
echo   - Go to Network Settings
echo   - Turn Server ON
echo   - Wait for joiner
echo.
echo ═══════════════════════════════════════════════════════════
echo TELL PC2 (JOINER):
echo.
echo   IP ADDRESS: %MYIP%
echo   JOIN CODE:  (shown in helper window)
echo.
echo ═══════════════════════════════════════════════════════════
echo.
echo Press any key to start the helper...
pause >nul

:: Check if helper exists
if exist "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe" (
    start "" "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe"
) else if exist "%~dp0sfo-helper.exe" (
    start "" "%~dp0sfo-helper.exe"
) else (
    echo [ERROR] sfo-helper.exe not found!
    echo.
    echo Expected locations:
    echo   %~dp0sfo-helper.exe
    echo   %~dp0..\SFO_Connectivity_Helper\sfo-helper.exe
    pause
    goto menu
)
goto menu

:join_test
cls
echo.
echo ═══════════════════════════════════════════════════════════
echo         PC2: JOIN TEST
echo ═══════════════════════════════════════════════════════════
echo.
set /p HOSTIP="Enter HOST PC's IP address: "
set /p JOINCODE="Enter JOIN CODE from host: "
echo.
echo STEP 1: Starting SFO Connectivity Helper as JOINER...
echo.
echo   When the helper starts:
echo   - Select option 2 (JOIN P2P) or option 4 (JOIN Relay)
echo   - Enter the code: %JOINCODE%
echo   - Enter the IP: %HOSTIP%
echo.
echo STEP 2: The game will launch automatically
echo.
echo STEP 3: In-game:
echo   - Go to Network Settings
echo   - Search for games
echo   - Join the host's game
echo.
echo ═══════════════════════════════════════════════════════════
echo.
echo Press any key to start the helper...
pause >nul

if exist "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe" (
    start "" "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe"
) else if exist "%~dp0sfo-helper.exe" (
    start "" "%~dp0sfo-helper.exe"
) else (
    echo [ERROR] sfo-helper.exe not found!
    pause
    goto menu
)
goto menu

:diagnose
cls
echo.
echo ═══════════════════════════════════════════════════════════
echo         NETWORK DIAGNOSTICS
echo ═══════════════════════════════════════════════════════════
echo.

echo [1/5] Your IP Addresses:
ipconfig | findstr /c:"IPv4"
echo.

echo [2/5] Testing port 1626...
netstat -an | findstr ":1626" >nul
if %errorlevel%==0 (
    echo   Port 1626: IN USE
    netstat -ano | findstr ":1626"
) else (
    echo   Port 1626: AVAILABLE
)
echo.

echo [3/5] Firewall status for SFO:
netsh advfirewall firewall show rule name=all | findstr /i "SFO" 2>nul
if %errorlevel%==1 (
    echo   No SFO firewall rules found
)
echo.

echo [4/5] Can reach other PC?
set /p OTHERIP="Enter other PC's IP (or skip): "
if not "%OTHERIP%"=="" (
    echo   Pinging %OTHERIP%...
    ping -n 2 %OTHERIP% | findstr /i "reply"
    echo.
    echo   Testing port 1626 on %OTHERIP%...
    powershell -Command "Test-NetConnection -ComputerName %OTHERIP% -Port 1626 -WarningAction SilentlyContinue | Select-Object TcpTestSucceeded"
)
echo.

echo [5/5] SFO Helper version:
if exist "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe" (
    "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe" version 2>nul
) else if exist "%~dp0sfo-helper.exe" (
    "%~dp0sfo-helper.exe" version 2>nul
) else (
    echo   sfo-helper.exe not found
)
echo.

echo ═══════════════════════════════════════════════════════════
pause
goto menu
