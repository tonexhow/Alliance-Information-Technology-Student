@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "ROOT=%CD%"
set "SERVER=%ROOT%\server"
set "OUT=%ROOT%\compile"
set "SYSTEM=%OUT%\system"

echo AITS Runtime Compiler
echo ----------------------------------------
echo Source : %ROOT%
echo Output : %OUT%
echo.

rem Compile only from a valid AITS source tree.
for %%F in ("client\package.json" "server\go.mod" "server\main.go" "data\database\SCHEMA.sql" ".env") do (
  if not exist "%ROOT%\%%~F" (
    echo [ERROR] Required source item missing: %%~F
    if /I not "%~1"=="--no-pause" pause
    exit /b 1
  )
)

where go.exe >nul 2>&1 || (
  echo [ERROR] Go is required.
  if /I not "%~1"=="--no-pause" pause
  exit /b 1
)
where robocopy.exe >nul 2>&1 || (
  echo [ERROR] robocopy is required.
  if /I not "%~1"=="--no-pause" pause
  exit /b 1
)

rem The compile folder is generated output only. Never point OUT at the project root.
if /I "%OUT%"=="%ROOT%" (
  echo [ERROR] Refusing unsafe output path.
  if /I not "%~1"=="--no-pause" pause
  exit /b 1
)
if exist "%OUT%" rmdir /s /q "%OUT%"

mkdir "%SYSTEM%\windows" >nul 2>&1
mkdir "%SYSTEM%\linux" >nul 2>&1
mkdir "%SYSTEM%\macos" >nul 2>&1
mkdir "%OUT%\data\database" >nul 2>&1
mkdir "%OUT%\data\library" >nul 2>&1
mkdir "%OUT%\data\private" >nul 2>&1
mkdir "%OUT%\data\logs" >nul 2>&1

copy /y "%ROOT%\.env" "%OUT%\.env" >nul || goto :fail
robocopy "%ROOT%\data\database" "%OUT%\data\database" /E /R:1 /W:1 /NFL /NDL /NJH /NJS /NP >nul
if errorlevel 8 goto :fail

pushd "%SERVER%"
call go mod verify
if errorlevel 1 (popd & goto :fail)

set "OLD_GOOS=%GOOS%"
set "OLD_GOARCH=%GOARCH%"
set "OLD_CGO=%CGO_ENABLED%"
set "CGO_ENABLED=0"
set "GOARCH=amd64"

echo [WINDOWS] Building backend and silent GUI launchers...
set "GOOS=windows"
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\windows\aits-server.exe" .
if errorlevel 1 (popd & goto :restorefail)
call go build -mod=readonly -trimpath -ldflags "-s -w -H=windowsgui" -o "%SYSTEM%\windows\start.exe" ./cmd/aits-start
if errorlevel 1 (popd & goto :restorefail)
call go build -mod=readonly -trimpath -ldflags "-s -w -H=windowsgui" -o "%SYSTEM%\windows\stop.exe" ./cmd/aits-stop
if errorlevel 1 (popd & goto :restorefail)

echo [LINUX] Building backend and launchers...
set "GOOS=linux"
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\linux\aits-server" .
if errorlevel 1 (popd & goto :restorefail)
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\linux\start" ./cmd/aits-start
if errorlevel 1 (popd & goto :restorefail)
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\linux\stop" ./cmd/aits-stop
if errorlevel 1 (popd & goto :restorefail)

echo [MACOS] Building backend and launchers...
set "GOOS=darwin"
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\macos\aits-server" .
if errorlevel 1 (popd & goto :restorefail)
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\macos\start" ./cmd/aits-start
if errorlevel 1 (popd & goto :restorefail)
call go build -mod=readonly -trimpath -ldflags "-s -w" -o "%SYSTEM%\macos\stop" ./cmd/aits-stop
if errorlevel 1 (popd & goto :restorefail)

set "GOOS=%OLD_GOOS%"
set "GOARCH=%OLD_GOARCH%"
set "CGO_ENABLED=%OLD_CGO%"
popd

>"%OUT%\README.txt" echo AITS compiled runtime
>>"%OUT%\README.txt" echo.
>>"%OUT%\README.txt" echo Windows: system\windows\start.exe and stop.exe use the Windows GUI subsystem and do not open a CMD window.
>>"%OUT%\README.txt" echo Linux: system\linux\start and stop.
>>"%OUT%\README.txt" echo macOS: system\macos\start and stop.
>>"%OUT%\README.txt" echo PostgreSQL configuration is read from .env.
>>"%OUT%\README.txt" echo Database schema files are under data\database.

echo.
echo [OK] Runtime created:
echo   %OUT%
echo.
echo Layout:
echo   compile\system\windows\
echo   compile\system\linux\
echo   compile\system\macos\
echo   compile\.env
echo   compile\data\
echo.
echo Windows start.exe and stop.exe are GUI-subsystem executables and remain silent.
if /I not "%~1"=="--no-pause" pause
exit /b 0

:restorefail
set "GOOS=%OLD_GOOS%"
set "GOARCH=%OLD_GOARCH%"
set "CGO_ENABLED=%OLD_CGO%"
:fail
echo.
echo [ERROR] Compilation stopped. Source files and data were not deleted.
if /I not "%~1"=="--no-pause" pause
exit /b 1
