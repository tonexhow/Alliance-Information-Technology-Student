@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "ROOT=%CD%"
set "CLIENT=%ROOT%\client"
set "ELECTRON=%ROOT%\wrapper\electron"
set "CAPACITOR=%ROOT%\wrapper\capacitor"
set "RELEASE=%ROOT%\release"

echo AITS Native Release Builder
echo ----------------------------------------
echo Windows installer : release\desktop\windows\AITS-Setup.exe
echo Android package   : release\mobile\android\AITS.apk
echo iOS package       : requires macOS + Xcode + Apple signing
echo.

rem Build the backend/runtime first. This creates compile\ only.
echo [1/6] Compiling AITS runtime...
call "%ROOT%\compile.bat" --no-pause
if errorlevel 1 goto :fail

rem release\ is generated output only.
if exist "%RELEASE%" rmdir /s /q "%RELEASE%"
mkdir "%RELEASE%\desktop\windows" >nul 2>&1
mkdir "%RELEASE%\desktop\linux" >nul 2>&1
mkdir "%RELEASE%\desktop\macos" >nul 2>&1
mkdir "%RELEASE%\mobile\android" >nul 2>&1
mkdir "%RELEASE%\mobile\ios" >nul 2>&1

where node.exe >nul 2>&1 || (echo [ERROR] Node.js is required.& goto :fail)
where npm.cmd >nul 2>&1 || (echo [ERROR] npm is required.& goto :fail)

for %%F in ("%CLIENT%\package.json" "%ELECTRON%\package.json" "%CAPACITOR%\package.json") do (
  if not exist "%%~F" (echo [ERROR] Missing wrapper source: %%~F& goto :fail)
)

echo [2/6] Building web client...
pushd "%CLIENT%"
if not exist node_modules\ call npm ci --include=dev --no-audit --no-fund
if errorlevel 1 (popd & goto :fail)
call npm run build
if errorlevel 1 (popd & goto :fail)
popd

echo [3/6] Building installable Windows desktop application...
pushd "%ELECTRON%"
if not exist node_modules\ call npm ci --include=dev --no-audit --no-fund
if errorlevel 1 (popd & goto :fail)
call npm run build:windows
if errorlevel 1 (popd & goto :fail)
popd

set "WIN_INSTALLER=%ROOT%\client\public\downloads\electron-build\AITS-Windows.exe"
if not exist "%WIN_INSTALLER%" (
  echo [ERROR] Electron did not produce AITS-Windows.exe.
  goto :fail
)
copy /y "%WIN_INSTALLER%" "%RELEASE%\desktop\windows\AITS-Setup.exe" >nul || goto :fail

echo [4/6] Preparing Android native project...
pushd "%CAPACITOR%"
if not exist node_modules\ call npm ci --include=dev --no-audit --no-fund
if errorlevel 1 (popd & goto :fail)
if not exist android\ (
  call npm run android:init
) else (
  call npm run android:sync
)
if errorlevel 1 (popd & goto :fail)
if not exist android\gradlew.bat (
  popd
  echo [ERROR] Android Gradle wrapper is unavailable.
  echo         Install/configure JDK and Android SDK, then run release.bat again.
  goto :fail
)

echo [5/6] Building installable Android APK...
pushd android
call gradlew.bat assembleDebug
if errorlevel 1 (popd & popd & goto :fail)
popd
popd

set "APK=%CAPACITOR%\android\app\build\outputs\apk\debug\app-debug.apk"
if not exist "%APK%" (
  echo [ERROR] Android build completed without app-debug.apk.
  goto :fail
)
copy /y "%APK%" "%RELEASE%\mobile\android\AITS.apk" >nul || goto :fail

echo [6/6] iOS release status...
>"%RELEASE%\mobile\ios\README.txt" echo AITS iOS release
>>"%RELEASE%\mobile\ios\README.txt" echo.
>>"%RELEASE%\mobile\ios\README.txt" echo A valid installable IPA cannot be produced by this Windows batch file.
>>"%RELEASE%\mobile\ios\README.txt" echo Build/sign the Capacitor iOS project on macOS with Xcode, an Apple Developer certificate, and a provisioning profile.
>>"%RELEASE%\mobile\ios\README.txt" echo The source is under wrapper\capacitor and supports ios:init / ios:sync.

echo.
echo [OK] Native release artifacts:
echo   %RELEASE%\desktop\windows\AITS-Setup.exe
echo   %RELEASE%\mobile\android\AITS.apk
echo.
echo [INFO] iOS IPA was not fabricated: Apple requires macOS/Xcode and valid signing.
echo [INFO] Android is currently debug-signed. Configure a release keystore before public distribution.
pause
exit /b 0

:fail
echo.
echo [ERROR] Release build stopped. Existing source and PostgreSQL data were not deleted.
pause
exit /b 1
