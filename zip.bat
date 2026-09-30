@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "ROOT=%CD%"
for %%I in ("%ROOT%") do set "PROJECT=%%~nxI"
for %%I in ("%ROOT%\..") do set "PARENT=%%~fI"

echo AITS Source Packager
echo ----------------------------------------

where powershell.exe >nul 2>&1 || (echo [ERROR] PowerShell is required.& pause & exit /b 1)
where robocopy.exe >nul 2>&1 || (echo [ERROR] robocopy is required.& pause & exit /b 1)
where tar.exe >nul 2>&1 || (echo [ERROR] tar.exe is required for ZIP verification.& pause & exit /b 1)

for %%F in ("client\package.json" "server\go.mod" "data\database\SCHEMA.sql" "run.bat" "clean.bat" "compile.bat" "release.bat" "gitpush.bat" ".gitignore") do (
  if not exist "%ROOT%\%%~F" (
    echo [ERROR] Required project item missing: %%~F
    pause
    exit /b 1
  )
)

set /a N=1
:findzip
set "ZIP=%PARENT%\%PROJECT%-%N%.zip"
if exist "%ZIP%" (
  set /a N+=1
  goto :findzip
)

set "STAGE=%TEMP%\%PROJECT%-package-%RANDOM%-%RANDOM%"
if exist "%STAGE%" rmdir /s /q "%STAGE%"
mkdir "%STAGE%\%PROJECT%" >nul 2>&1

echo [COPY] Creating a clean overwrite-ready staging copy...
echo        Only the approved AITS root structure is packaged.

for %%D in (client server data wrapper) do (
  robocopy "%ROOT%\%%D" "%STAGE%\%PROJECT%\%%D" /E /R:1 /W:1 /NFL /NDL /NJH /NJS /NP ^
    /XD node_modules dist .vite .git coverage cache __pycache__ .next .cache out compile compiled release releases build .gradle Pods DerivedData ^
    /XF *.tmp *.log *.tsbuildinfo *.test.exe coverage.out coverage.txt .env .env.example >nul
  set "RC=!ERRORLEVEL!"
  if !RC! GEQ 8 (
    echo [ERROR] Staging %%D failed with robocopy code !RC!.
    goto :cleanupfail
  )
)

for %%F in (.env .env.example .gitignore run.bat zip.bat clean.bat compile.bat release.bat gitpush.bat) do (
  if exist "%ROOT%\%%F" copy /y "%ROOT%\%%F" "%STAGE%\%PROJECT%\%%F" >nul
)

echo [ZIP] Creating:
echo %ZIP%

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command ^
  "Compress-Archive -LiteralPath '%STAGE%\%PROJECT%' -DestinationPath '%ZIP%' -CompressionLevel Optimal -Force"

if errorlevel 1 (
  echo [ERROR] ZIP creation failed.
  goto :cleanupfail
)

set "LIST=%TEMP%\%PROJECT%-zip-list-%RANDOM%.txt"
tar.exe -tf "%ZIP%" > "%LIST%" 2>nul
if errorlevel 1 (
  echo [ERROR] ZIP verification failed.
  if exist "%ZIP%" del /q "%ZIP%" >nul 2>&1
  goto :cleanupfail
)

for %%P in (/node_modules/ /dist/ /.vite/ /.git/ /coverage/ /.cache/) do (
  findstr /i /c:"%%P" "%LIST%" >nul && (
    echo [ERROR] Generated path %%P was found inside the ZIP.
    del /q "%ZIP%" >nul 2>&1
    goto :cleanupfail
  )
)

if exist "%LIST%" del /q "%LIST%" >nul 2>&1
if exist "%STAGE%" rmdir /s /q "%STAGE%" >nul 2>&1

echo.
echo [OK] Created and verified:
echo %ZIP%
echo.
echo Packaged root:
echo   client\
echo   server\
echo   data\
echo   wrapper\
echo   .env
echo   .env.example
echo   run.bat
echo   zip.bat
echo   clean.bat
echo   compile.bat
echo   release.bat
echo   gitpush.bat
echo   .gitignore
echo.
pause
exit /b 0

:cleanupfail
if defined LIST if exist "%LIST%" del /q "%LIST%" >nul 2>&1
if defined STAGE if exist "%STAGE%" rmdir /s /q "%STAGE%" >nul 2>&1
pause
exit /b 1
