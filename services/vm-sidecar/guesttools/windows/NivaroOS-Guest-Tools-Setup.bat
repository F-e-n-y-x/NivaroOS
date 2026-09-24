@echo off
setlocal EnableExtensions EnableDelayedExpansion
title NivaroOS Guest Tools
cd /d "%~dp0"

rem NivaroOS Guest Tools for Windows 10/11 (and Server 2016+).
rem Installs every VirtIO driver + the QEMU guest agent (virtio-win),
rem the SPICE agent (copy/paste with the NivaroOS console), WinFsp, and
rem starts the VirtIO-FS service so the NivaroOS shared folder
rem (/DATA/VMs/share on the server) shows up as a drive.
rem Safe to run again: every step is skipped/repaired when already done.
rem Unattended: NivaroOS-Guest-Tools-Setup.bat /quiet

set "QUIET="
if /i "%~1"=="/quiet" set "QUIET=1"
set "LOG=%TEMP%\nivaroos-guest-tools.log"
set "FAILED="

net session >nul 2>&1
if errorlevel 1 (
    if defined QUIET (
        echo Administrator rights are required.
        exit /b 5
    )
    echo Asking for administrator permission...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)

echo NivaroOS Guest Tools setup - %DATE% %TIME% > "%LOG%"
echo ==================================================================
echo   NivaroOS Guest Tools - drivers, clipboard and shared folder
echo ==================================================================
echo.

rem --- architecture / Windows version (for the VirtIO-FS driver folder)
set "ARCH=amd64"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "ARCH=ARM64"
if /i "%PROCESSOR_ARCHITECTURE%"=="x86" if not defined PROCESSOR_ARCHITEW6432 set "ARCH=x86"
for /f "tokens=3" %%b in ('reg query "HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion" /v CurrentBuildNumber 2^>nul ^| find "CurrentBuildNumber"') do set "BUILD=%%b"
set "WINDIR_TAG=w10"
if defined BUILD if !BUILD! GEQ 22000 set "WINDIR_TAG=w11"
echo Windows build !BUILD!, !ARCH! >> "%LOG%"

rem --- 1. VirtIO drivers + QEMU guest agent (+ VirtIO-FS service binary)
echo [1/5] Installing VirtIO drivers and the QEMU guest agent...
if exist "%~dp0virtio-win-guest-tools.exe" (
    start /wait "" "%~dp0virtio-win-guest-tools.exe" /install /passive /norestart
    set "RC=!errorlevel!"
    echo virtio-win-guest-tools exit !RC! >> "%LOG%"
    rem 0 = done, 3010 = done (reboot pending), 1638 = already installed
    if not "!RC!"=="0" if not "!RC!"=="3010" if not "!RC!"=="1638" (
        echo       [!] The driver installer returned !RC! - see %LOG%
        set "FAILED=1"
    )
) else (
    echo       [!] virtio-win-guest-tools.exe is missing from this disc.
    set "FAILED=1"
)

rem --- 2. SPICE agent: carries the clipboard between the NivaroOS console
rem     and Windows (over the VM's com.redhat.spice.0 port, whose VirtIO
rem     serial driver step 1 installed). Only built for x64; a disc built
rem     while the server couldn't download it just lacks it.
echo [2/5] Installing the SPICE agent (console copy/paste)...
sc query spice-agent >nul 2>&1
if not errorlevel 1 (
    echo       already installed
) else if not "!ARCH!"=="amd64" (
    echo       skipped - only available for 64-bit Intel/AMD Windows
    echo spice-vdagent skipped: !ARCH! >> "%LOG%"
) else if exist "%~dp0spice-vdagent-x64.msi" (
    msiexec /i "%~dp0spice-vdagent-x64.msi" /qn /norestart /l*v "%TEMP%\nivaroos-spice-vdagent.log"
    set "RC=!errorlevel!"
    echo spice-vdagent msiexec exit !RC! >> "%LOG%"
    if not "!RC!"=="0" if not "!RC!"=="3010" if not "!RC!"=="1638" (
        echo       [!] The SPICE agent did not install - see %TEMP%\nivaroos-spice-vdagent.log
        set "FAILED=1"
    )
) else (
    echo       skipped - spice-vdagent-x64.msi isn't on this disc, so copy/paste
    echo       with the console won't work. Rebuild the Guest Tools in NivaroOS.
    echo spice-vdagent.msi missing from the disc >> "%LOG%"
)

rem --- 3. WinFsp (the file system layer VirtIO-FS needs)
echo [3/5] Installing WinFsp...
if exist "%ProgramFiles(x86)%\WinFsp\bin\winfsp-x64.dll" (
    echo       already installed
) else if exist "%~dp0winfsp.msi" (
    msiexec /i "%~dp0winfsp.msi" /qn /norestart /l*v "%TEMP%\nivaroos-winfsp.log"
    echo winfsp msiexec exit !errorlevel! >> "%LOG%"
    if not exist "%ProgramFiles(x86)%\WinFsp\bin\winfsp-x64.dll" if not exist "%ProgramFiles%\WinFsp\bin\winfsp-x64.dll" (
        echo       [!] WinFsp did not install - see %TEMP%\nivaroos-winfsp.log
        set "FAILED=1"
    )
) else (
    echo       [!] winfsp.msi is missing from this disc.
    set "FAILED=1"
)

rem --- 4. VirtIO-FS driver (normally already added by step 1)
echo [4/5] Installing the VirtIO-FS driver...
if exist "%~dp0viofs\%WINDIR_TAG%\%ARCH%\viofs.inf" (
    pnputil /add-driver "%~dp0viofs\%WINDIR_TAG%\%ARCH%\viofs.inf" /install >> "%LOG%" 2>&1
) else if exist "%~dp0viofs\w10\%ARCH%\viofs.inf" (
    pnputil /add-driver "%~dp0viofs\w10\%ARCH%\viofs.inf" /install >> "%LOG%" 2>&1
)

rem --- 5. VirtIO-FS service: create if the driver package didn't, then start
echo [5/5] Starting the shared folder service...
set "VIOFS_EXE=%ProgramFiles%\Virtio-Win\VioFS\virtiofs.exe"
if not exist "%VIOFS_EXE%" if exist "%~dp0viofs\%WINDIR_TAG%\%ARCH%\virtiofs.exe" (
    mkdir "%ProgramFiles%\Virtio-Win\VioFS" >nul 2>&1
    copy /y "%~dp0viofs\%WINDIR_TAG%\%ARCH%\virtiofs.exe" "%VIOFS_EXE%" >nul
)
sc query VirtioFsSvc >nul 2>&1
if errorlevel 1 (
    sc create VirtioFsSvc binPath= "\"%VIOFS_EXE%\"" start= auto depend= "WinFsp.Launcher/VirtioFsDrv" DisplayName= "Virtio FS Service" >> "%LOG%" 2>&1
)
sc config VirtioFsSvc start= auto >> "%LOG%" 2>&1
sc start VirtioFsSvc >> "%LOG%" 2>&1
timeout /t 4 /nobreak >nul
sc query VirtioFsSvc | find "RUNNING" >nul
if errorlevel 1 (
    echo.
    echo       [!] The shared folder service did not start.
    echo           Usually this means the VM has no shared-folder device yet:
    echo           shut the VM down, start it again from NivaroOS, then run
    echo           this setup once more. Details: %LOG%
    set "FAILED=1"
) else (
    echo       running - the NivaroOS shared folder is now a drive in File Explorer.
)

echo.
if defined FAILED (
    echo ==================================================================
    echo   Finished with problems - see the [!] lines above.
    echo ==================================================================
    if not defined QUIET pause
    exit /b 1
)
echo ==================================================================
echo   Done. Drivers are installed and the shared folder is available
echo   in File Explorer ("This PC"). A restart finishes driver updates
echo   (and turns on copy/paste with the NivaroOS console).
echo ==================================================================
if not defined QUIET timeout /t 8
exit /b 0
