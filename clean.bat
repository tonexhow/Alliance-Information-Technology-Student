@echo off
setlocal EnableExtensions
cd /d "%~dp0"

set "ROOT=%CD%"

echo AITS Safe Source Cleaner
echo ----------------------------------------
echo Project: %ROOT%
echo.

rem Hard safety checks: refuse to run outside the expected AITS tree.
if not exist "%ROOT%\client\package.json" goto :wrongroot
if not exist "%ROOT%\server\go.mod" goto :wrongroot
if not exist "%ROOT%\data\database\SCHEMA.sql" goto :wrongroot

rem This challenge prevents accidental cleanup even for a developer.
echo [SAFETY] clean.bat NEVER connects to PostgreSQL.
echo [SAFETY] clean.bat NEVER deletes the data\ tree.
echo [SAFETY] It removes only generated dependencies, caches, build/release output, logs,
echo          and specifically-known obsolete duplicate source files.
echo.
set "CLEAN_CHALLENGE="
set /p "CLEAN_CHALLENGE=Type CLEAN-SOURCE to continue: "
if /I not "%CLEAN_CHALLENGE%"=="CLEAN-SOURCE" (
  echo [CANCELLED] Nothing was deleted.
  pause
  exit /b 0
)
echo.

echo [1/6] Removing frontend generated files...
for %%D in (
  "client\node_modules"
  "client\dist"
  "client\.vite"
  "client\coverage"
  "client\.cache"
) do if exist "%ROOT%\%%~D" rmdir /s /q "%ROOT%\%%~D"
for /r "%ROOT%\client" %%F in (*.tsbuildinfo *.tmp *.log) do del /q "%%F" >nul 2>&1

echo [2/6] Removing wrapper dependency/build caches...
for %%D in (
  "wrapper\electron\node_modules"
  "wrapper\electron\dist"
  "wrapper\electron\out"
  "wrapper\electron\release"
  "wrapper\capacitor\node_modules"
  "wrapper\capacitor\dist"
  "wrapper\capacitor\www"
  "wrapper\capacitor\android\.gradle"
  "wrapper\capacitor\android\build"
  "wrapper\capacitor\android\app\build"
  "wrapper\capacitor\ios\App\Pods"
  "wrapper\capacitor\ios\DerivedData"
  "client\public\downloads\electron-build"
) do if exist "%ROOT%\%%~D" rmdir /s /q "%ROOT%\%%~D"

echo [3/6] Removing generated compile/release output...
if exist "%ROOT%\compile" rmdir /s /q "%ROOT%\compile"
if exist "%ROOT%\compiled" rmdir /s /q "%ROOT%\compiled"
if exist "%ROOT%\release" rmdir /s /q "%ROOT%\release"
if exist "%ROOT%\releases" rmdir /s /q "%ROOT%\releases"

echo [4/6] Removing temporary server artifacts...
for /r "%ROOT%\server" %%F in (*.tmp *.log *.test.exe coverage.out coverage.txt) do del /q "%%F" >nul 2>&1
if exist "%ROOT%\server\aits.exe" del /q "%ROOT%\server\aits.exe"
if exist "%ROOT%\server\server.exe" del /q "%ROOT%\server\server.exe"

echo [5/6] Removing only known obsolete duplicate files...
if exist "%ROOT%\client\src\MemberPage.tsx" del /q "%ROOT%\client\src\MemberPage.tsx"
if exist "%ROOT%\client\src\MemberPage.css" del /q "%ROOT%\client\src\MemberPage.css"
if exist "%ROOT%\server\.env" del /q "%ROOT%\server\.env"
if exist "%ROOT%\server\.env.example" del /q "%ROOT%\server\.env.example"
rem Old stop-process implementations conflict with proc_windows.go/proc_unix.go.
if exist "%ROOT%\server\cmd\aits-stop\process_windows.go" del /q "%ROOT%\server\cmd\aits-stop\process_windows.go"
if exist "%ROOT%\server\cmd\aits-stop\process_unix.go" del /q "%ROOT%\server\cmd\aits-stop\process_unix.go"

echo [6/6] Removing root temp/log leftovers...
for %%F in ("%ROOT%\*.tmp" "%ROOT%\*.log") do if exist "%%~F" del /q "%%~F" >nul 2>&1

echo.
echo [OK] Safe cleanup complete.
echo.
echo Preserved:
echo   client\ source
echo   server\ source
echo   data\ ^(ALL PostgreSQL schema, library and private content^)
echo   wrapper\ source
echo   .env and .env.example
echo   all project .bat files
echo   .gitignore and Git repository metadata
echo.
echo No SQL command was executed.
pause
exit /b 0

:wrongroot
echo [ERROR] This does not look like the AITS project root.
echo         Nothing was deleted.
pause
exit /b 1
