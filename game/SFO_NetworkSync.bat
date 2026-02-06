@echo off
title SFO Network Sync Setup
echo ═══════════════════════════════════════════════════════════
echo            SFO NETWORK SYNC SETUP
echo ═══════════════════════════════════════════════════════════
echo.

:: Get local IP
for /f "tokens=2 delims=:" %%a in ('ipconfig ^| findstr /c:"IPv4"') do (
    set LOCALIP=%%a
    goto :gotip
)
:gotip
set LOCALIP=%LOCALIP: =%

echo Your IP: %LOCALIP%
echo.
echo This script sets up file sharing so both PCs stay in sync.
echo.
echo ═══════════════════════════════════════════════════════════
echo OPTIONS:
echo.
echo   1. SETUP THIS PC AS HOST (share the SFO folder)
echo   2. CONNECT TO HOST PC (access shared folder)
echo   3. SYNC FILES NOW (copy changes to/from network)
echo   4. CREATE PORTABLE USB PACKAGE
echo   5. Exit
echo.
echo ═══════════════════════════════════════════════════════════
echo.

:menu
set /p choice="Select option (1-5): "

if "%choice%"=="1" goto setup_host
if "%choice%"=="2" goto connect_host
if "%choice%"=="3" goto sync_now
if "%choice%"=="4" goto create_usb
if "%choice%"=="5" goto exit

echo Invalid choice.
goto menu

:setup_host
echo.
echo Setting up this PC as the SFO host...
echo.

:: Share the SFO_Bundle folder
net share SFO_Game="%~dp0" /grant:Everyone,FULL 2>nul
if %errorlevel%==0 (
    echo [SUCCESS] Folder shared as: \\%COMPUTERNAME%\SFO_Game
) else (
    echo [INFO] Share may already exist or needs admin rights
)

:: Enable network discovery
netsh advfirewall firewall set rule group="File and Printer Sharing" new enable=Yes 2>nul
netsh advfirewall firewall set rule group="Network Discovery" new enable=Yes 2>nul

echo.
echo ═══════════════════════════════════════════════════════════
echo  SHARE INFO - GIVE THIS TO THE OTHER PC:
echo ═══════════════════════════════════════════════════════════
echo.
echo   Network Path: \\%LOCALIP%\SFO_Game
echo   Or:           \\%COMPUTERNAME%\SFO_Game
echo.
echo   On the other PC, run this script and choose option 2
echo   Then enter: %LOCALIP%
echo.
echo ═══════════════════════════════════════════════════════════
pause
goto menu

:connect_host
echo.
set /p HOSTIP="Enter the HOST PC's IP address: "
echo.
echo Connecting to \\%HOSTIP%\SFO_Game ...
echo.

:: Test connection
if exist "\\%HOSTIP%\SFO_Game\Street Fighter Online.exe" (
    echo [SUCCESS] Connected to host!
    echo.
    echo You can now:
    echo   1. Run the game directly from: \\%HOSTIP%\SFO_Game\Street Fighter Online.exe
    echo   2. Run the helper from: \\%HOSTIP%\SFO_Game\sfo-helper.exe
    echo.
    echo Or map as a drive for easier access:
    net use Z: "\\%HOSTIP%\SFO_Game" /persistent:yes 2>nul
    if %errorlevel%==0 (
        echo [SUCCESS] Mapped to drive Z:
        echo   Game: Z:\Street Fighter Online.exe
        echo   Helper: Z:\sfo-helper.exe
    )
) else (
    echo [ERROR] Could not connect to \\%HOSTIP%\SFO_Game
    echo.
    echo Make sure:
    echo   - The host PC ran option 1
    echo   - Both PCs are on the same network
    echo   - Firewall allows file sharing
)
echo.
pause
goto menu

:sync_now
echo.
echo ═══════════════════════════════════════════════════════════
echo SYNC OPTIONS:
echo.
echo   1. PUSH: Copy MY files TO network share
echo   2. PULL: Copy FROM network share TO my PC
echo   3. Back
echo.
set /p syncopt="Select (1-3): "

if "%syncopt%"=="1" goto sync_push
if "%syncopt%"=="2" goto sync_pull
if "%syncopt%"=="3" goto menu

:sync_push
set /p HOSTIP="Enter HOST IP to push to: "
echo.
echo Pushing files to \\%HOSTIP%\SFO_Game ...
robocopy "%~dp0" "\\%HOSTIP%\SFO_Game" /MIR /XD .git bz_extracted installer_extracted /XF *.log *.tmp
echo.
echo [DONE] Files pushed to network.
pause
goto menu

:sync_pull
set /p HOSTIP="Enter HOST IP to pull from: "
echo.
echo Pulling files from \\%HOSTIP%\SFO_Game ...
robocopy "\\%HOSTIP%\SFO_Game" "%~dp0" /MIR /XD .git bz_extracted installer_extracted /XF *.log *.tmp
echo.
echo [DONE] Files pulled from network.
pause
goto menu

:create_usb
echo.
echo ═══════════════════════════════════════════════════════════
echo CREATE PORTABLE USB PACKAGE
echo ═══════════════════════════════════════════════════════════
echo.
echo This will copy all necessary files to a USB drive.
echo.

:: List available drives
echo Available drives:
wmic logicaldisk get caption,description,volumename 2>nul | findstr /i "removable"
echo.

set /p USBDRIVE="Enter USB drive letter (e.g., E): "
set USBPATH=%USBDRIVE%:\SFO_Portable

echo.
echo Creating portable package at %USBPATH% ...
echo.

:: Create directory
mkdir "%USBPATH%" 2>nul

:: Copy essential files
echo Copying game files...
robocopy "%~dp0" "%USBPATH%" "Street Fighter Online.exe" "config.sfo" "sfo-helper.exe" /NFL /NDL /NJH /NJS
robocopy "%~dp0" "%USBPATH%" "*.dll" /NFL /NDL /NJH /NJS
robocopy "%~dp0" "%USBPATH%" "SFO_NetworkHelper.bat" "SFO_LocalRelay.ps1" /NFL /NDL /NJH /NJS

:: Copy casts folder if exists
if exist "%~dp0casts" (
    echo Copying game assets...
    robocopy "%~dp0casts" "%USBPATH%\casts" /E /NFL /NDL /NJH /NJS
)

:: Copy characters folder if exists
if exist "%~dp0characters" (
    robocopy "%~dp0characters" "%USBPATH%\characters" /E /NFL /NDL /NJH /NJS
)

:: Copy connectivity helper
echo Copying SFO Connectivity Helper...
if exist "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe" (
    copy "%~dp0..\SFO_Connectivity_Helper\sfo-helper.exe" "%USBPATH%\" >nul
)

:: Create launcher script for USB
echo @echo off > "%USBPATH%\PLAY_SFO.bat"
echo title SFO Portable >> "%USBPATH%\PLAY_SFO.bat"
echo cd /d "%%~dp0" >> "%USBPATH%\PLAY_SFO.bat"
echo echo Starting Street Fighter Online... >> "%USBPATH%\PLAY_SFO.bat"
echo start "" "Street Fighter Online.exe" >> "%USBPATH%\PLAY_SFO.bat"

echo @echo off > "%USBPATH%\RUN_HELPER.bat"
echo title SFO Helper >> "%USBPATH%\RUN_HELPER.bat"
echo cd /d "%%~dp0" >> "%USBPATH%\RUN_HELPER.bat"
echo sfo-helper.exe >> "%USBPATH%\RUN_HELPER.bat"
echo pause >> "%USBPATH%\RUN_HELPER.bat"

echo.
echo ═══════════════════════════════════════════════════════════
echo [SUCCESS] Portable package created at: %USBPATH%
echo.
echo Contents:
dir /b "%USBPATH%"
echo.
echo To use on another PC:
echo   1. Plug in USB drive
echo   2. Open %USBDRIVE%:\SFO_Portable
echo   3. Double-click PLAY_SFO.bat or RUN_HELPER.bat
echo ═══════════════════════════════════════════════════════════
pause
goto menu

:exit
echo Goodbye!
