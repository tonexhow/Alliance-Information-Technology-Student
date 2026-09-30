@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "ROOT=%CD%"
set "CLIENT_DIR=%ROOT%\client"
set "SERVER_DIR=%ROOT%\server"
set "GLOBAL_ENV=%ROOT%\..\[BOOT] Scripts\global.env"
set "SCHEMA_FILE=%ROOT%\data\database\SCHEMA.sql"
set "API_PORT=3013"
set "CLIENT_PORT=3014"
if exist "%ROOT%\.env" (
  for /f "tokens=1,* delims==" %%A in ('findstr /R /B /C:"SERVER_PORT=" /C:"CLIENT_PORT=" "%ROOT%\.env" 2^>nul') do (
    if /I "%%A"=="SERVER_PORT" if not "%%B"=="" set "API_PORT=%%B"
    if /I "%%A"=="CLIENT_PORT" if not "%%B"=="" set "CLIENT_PORT=%%B"
  )
)

echo AITS Development Launcher
echo ----------------------------------------
echo API    : http://127.0.0.1:%API_PORT%
echo Client : http://127.0.0.1:%CLIENT_PORT%
echo.

where node >nul 2>&1 || (echo [ERROR] Node.js is not available.& pause & exit /b 1)
where npm >nul 2>&1 || (echo [ERROR] npm is not available.& pause & exit /b 1)
where go >nul 2>&1 || (echo [ERROR] Go is not available.& pause & exit /b 1)
where curl.exe >nul 2>&1 || (echo [ERROR] curl.exe is not available.& pause & exit /b 1)

if not exist "%CLIENT_DIR%\package.json" (
  echo [ERROR] client\package.json was not found.
  pause
  exit /b 1
)

if not exist "%SERVER_DIR%\main.go" (
  echo [ERROR] server\main.go was not found.
  pause
  exit /b 1
)

if not exist "%SERVER_DIR%\go.mod" (
  echo [ERROR] server\go.mod was not found.
  pause
  exit /b 1
)

if not exist "%SCHEMA_FILE%" (
  echo [ERROR] Missing PostgreSQL schema: %SCHEMA_FILE%
  pause
  exit /b 1
)

echo [CONFIG] Checking environment configuration...
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command ^
  "$cfg=@{}; $local='%ROOT%\.env'; $global='%GLOBAL_ENV%'; function Read-FallbackEnv([string]$path,[bool]$fallbackOnly){ if(Test-Path -LiteralPath $path){ Get-Content -LiteralPath $path | ForEach-Object { $line=$_.Trim(); if($line -and -not $line.StartsWith('#') -and $line.Contains('=')){ $pair=$line.Split('=',2); $key=$pair[0].Trim(); $value=$pair[1].Trim().Trim('"').Trim([char]39); if($key -and $value -and ((-not $fallbackOnly) -or (-not $cfg.ContainsKey($key)))){ $cfg[$key]=$value } } } } }; Read-FallbackEnv $local $false; Read-FallbackEnv $global $true; foreach($k in @('DATABASE_URL','SMTP_EMAIL','SMTP_PASSWORD','GOOGLE_CLIENT_ID','GOOGLE_CLIENT_SECRET','GITHUB_CLIENT_ID','GITHUB_CLIENT_SECRET')){ $ev=(Get-Item -Path ('Env:'+ $k) -ErrorAction SilentlyContinue).Value; if($ev){$cfg[$k]=$ev} }; if(-not $cfg.ContainsKey('DATABASE_URL') -or $cfg['DATABASE_URL'] -match 'CHANGE_ME'){ Write-Host '[ERROR] DATABASE_URL is missing. Checked aits\.env, then [BOOT] Scripts\global.env.' -ForegroundColor Red; exit 2 }; if(Test-Path -LiteralPath $local){Write-Host '[CONFIG] Local credentials: root .env' -ForegroundColor DarkGray}; if(Test-Path -LiteralPath $global){Write-Host '[CONFIG] Global fallback: [BOOT] Scripts\global.env' -ForegroundColor DarkGray}; Write-Host '[OK] PostgreSQL configuration found.' -ForegroundColor Green; if(-not ($cfg.ContainsKey('SMTP_EMAIL') -and $cfg.ContainsKey('SMTP_PASSWORD'))){Write-Host '[WARN] Email credentials are not configured.' -ForegroundColor Yellow}else{Write-Host '[OK] Email credentials found.' -ForegroundColor Green}; if(-not ($cfg.ContainsKey('GOOGLE_CLIENT_ID') -and $cfg.ContainsKey('GOOGLE_CLIENT_SECRET'))){Write-Host '[WARN] Google visitor sign-in credentials are not configured.' -ForegroundColor Yellow}else{Write-Host '[OK] Google visitor sign-in configured.' -ForegroundColor Green}; if(-not ($cfg.ContainsKey('GITHUB_CLIENT_ID') -and $cfg.ContainsKey('GITHUB_CLIENT_SECRET'))){Write-Host '[WARN] GitHub visitor sign-in credentials are not configured.' -ForegroundColor Yellow}else{Write-Host '[OK] GitHub visitor sign-in configured.' -ForegroundColor Green}; Write-Host '[INFO] Root .env loaded for application, database, mail and OAuth settings.' -ForegroundColor Cyan"
if errorlevel 2 (
  pause
  exit /b 1
)

if not exist "%CLIENT_DIR%\node_modules\.bin\vite.cmd" (
  echo [CLIENT] Dependencies are missing. Installing...
  pushd "%CLIENT_DIR%"
  if exist "package-lock.json" (
    call npm ci --include=dev --no-audit --no-fund
  ) else (
    call npm install --include=dev --no-audit --no-fund
  )
  if errorlevel 1 (
    popd
    echo [ERROR] Failed to install client dependencies.
    pause
    exit /b 1
  )
  popd
)

echo [SERVER] Checking Go modules...
pushd "%SERVER_DIR%"
call go mod verify
if errorlevel 1 (
  popd
  echo [ERROR] Go module verification failed. Run ^"go mod tidy^" manually only when dependencies intentionally change.
  pause
  exit /b 1
)
echo [SERVER] Go modules are ready.
echo [SERVER] Compiling backend preflight...
set "PREFLIGHT_BIN=%TEMP%\aits-preflight-%RANDOM%-%RANDOM%.exe"
call go build -mod=readonly -o "!PREFLIGHT_BIN!" .
if errorlevel 1 (
  if exist "!PREFLIGHT_BIN!" del /q "!PREFLIGHT_BIN!" >nul 2>&1
  popd
  echo [ERROR] Backend compile preflight failed.
  echo         Fix the Go compiler errors shown above before starting AITS.
  pause
  exit /b 1
)
if exist "!PREFLIGHT_BIN!" del /q "!PREFLIGHT_BIN!" >nul 2>&1
popd
echo [SERVER] Backend compile preflight passed.

echo [CLIENT] Running TypeScript + Vite build preflight...
pushd "%CLIENT_DIR%"
call npm run build
if errorlevel 1 (
  popd
  echo [ERROR] Client build preflight failed.
  echo         Fix the TypeScript/Vite errors shown above before starting AITS.
  pause
  exit /b 1
)
if exist "dist" rmdir /s /q "dist" >nul 2>&1
popd
echo [CLIENT] Build preflight passed.

echo [SERVER] Starting Go backend...
echo [DEV] Test accounts are opt-in. Set AITS_DEV_TEST_ACCOUNTS=1 only when you intentionally need them.

rem Launch through a tiny wrapper so the launcher can detect a backend that
rem exits during startup instead of waiting for the full health-check timeout.
set "SERVER_STATUS=%TEMP%\aits-server-status-%RANDOM%-%RANDOM%.txt"
set "SERVER_WRAPPER=%TEMP%\aits-server-start-%RANDOM%-%RANDOM%.bat"
if exist "!SERVER_STATUS!" del /q "!SERVER_STATUS!" >nul 2>&1
if exist "!SERVER_WRAPPER!" del /q "!SERVER_WRAPPER!" >nul 2>&1
>"!SERVER_WRAPPER!" echo @echo off
>>"!SERVER_WRAPPER!" echo cd /d "%SERVER_DIR%"
>>"!SERVER_WRAPPER!" echo go run -mod=readonly .
>>"!SERVER_WRAPPER!" echo set "AITS_EXIT=%%ERRORLEVEL%%"
>>"!SERVER_WRAPPER!" echo ^>"!SERVER_STATUS!" echo %%AITS_EXIT%%
>>"!SERVER_WRAPPER!" echo if not "%%AITS_EXIT%%"=="0" echo.
>>"!SERVER_WRAPPER!" echo if not "%%AITS_EXIT%%"=="0" echo [ERROR] AITS backend exited with code %%AITS_EXIT%%.
>>"!SERVER_WRAPPER!" echo if not "%%AITS_EXIT%%"=="0" echo Review the error above, then press any key to close this server window.
>>"!SERVER_WRAPPER!" echo if not "%%AITS_EXIT%%"=="0" pause ^>nul
>>"!SERVER_WRAPPER!" echo exit /b %%AITS_EXIT%%
start "AITS Server" cmd /c ""!SERVER_WRAPPER!""

echo [SERVER] Waiting for AITS services...
set /a ATTEMPTS=0
:waitserver
set /a ATTEMPTS+=1
curl.exe -fsS --max-time 2 "http://127.0.0.1:%API_PORT%/api/health" >nul 2>&1
if not errorlevel 1 goto :serverready
if exist "!SERVER_STATUS!" (
  set "SERVER_EXIT="
  set /p SERVER_EXIT=<"!SERVER_STATUS!"
  echo [ERROR] The AITS backend exited during startup with code !SERVER_EXIT!.
  echo         Check the AITS Server window for the exact backend error.
  del /q "!SERVER_STATUS!" >nul 2>&1
  del /q "!SERVER_WRAPPER!" >nul 2>&1
  pause
  exit /b 1
)
if !ATTEMPTS! GEQ 45 (
  echo [ERROR] The AITS backend did not become ready on port %API_PORT% after 45 seconds.
  echo         Check the AITS Server window for the exact error.
  pause
  exit /b 1
)
timeout /t 1 /nobreak >nul
goto :waitserver

:serverready
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command ^
  "$h = Invoke-RestMethod -Uri 'http://127.0.0.1:%API_PORT%/api/health' -TimeoutSec 4; if (-not $h.database) { exit 1 }"
if errorlevel 1 (
  echo [ERROR] PostgreSQL is not connected.
  echo         Confirm DATABASE_URL in root .env or [BOOT] Scripts\global.env and make sure PostgreSQL is running.
  pause
  exit /b 1
)
echo [DATABASE] PostgreSQL connected and ready.

echo [CLIENT] Starting React + Vite...
start "AITS Client" /D "%CLIENT_DIR%" cmd /k "npm run dev -- --host 127.0.0.1 --port %CLIENT_PORT% --strictPort"

echo [CLIENT] Waiting for the interface...
set /a CLIENT_ATTEMPTS=0
:waitclient
set /a CLIENT_ATTEMPTS+=1
curl.exe -fsS --max-time 2 "http://127.0.0.1:%CLIENT_PORT%/" >nul 2>&1
if not errorlevel 1 goto :clientready
if !CLIENT_ATTEMPTS! GEQ 45 (
  echo [ERROR] The AITS interface did not become ready on port %CLIENT_PORT%.
  echo         Check the AITS Client window for the exact error.
  pause
  exit /b 1
)
timeout /t 1 /nobreak >nul
goto :waitclient

:clientready
echo.
echo [READY]
echo Frontend : http://127.0.0.1:%CLIENT_PORT%
echo Backend  : http://127.0.0.1:%API_PORT%
echo Database : connected
echo.
start "" "http://127.0.0.1:%CLIENT_PORT%"
pause
exit /b 0
