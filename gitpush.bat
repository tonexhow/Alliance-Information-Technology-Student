@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

set "ROOT=%CD%"
set "REMOTE=https://github.com/tonexhow/Alliance-Information-Technology-Student.git"

echo AITS GitHub Push
echo ----------------------------------------
echo Repository:
echo %REMOTE%
echo.

where git.exe >nul 2>&1 || (
  echo [ERROR] Git is not installed or is not available in PATH.
  pause
  exit /b 1
)

rem Refuse to run from the wrong directory.
if not exist "%ROOT%\.gitignore" (
  echo [ERROR] .gitignore is missing. Refusing to stage files.
  pause
  exit /b 1
)
if not exist "%ROOT%\client\package.json" (
  echo [ERROR] This does not look like the AITS project root.
  pause
  exit /b 1
)

rem If .env was tracked by an older repository state, remove it from the index only.
git ls-files --error-unmatch .env >nul 2>&1
if not errorlevel 1 (
  echo [SAFETY] Removing tracked .env from the Git index. Your local file is kept.
  git rm --cached --ignore-unmatch .env >nul 2>&1
)

rem Verify the most important secret matches .gitignore, even before the repository exists.
git check-ignore --no-index -q .env
if errorlevel 1 (
  echo [ERROR] .env is NOT ignored. Push cancelled for safety.
  pause
  exit /b 1
)

if not exist README.md (
  >README.md echo # Alliance-Information-Technology-Student
)

if not exist .git\ (
  echo [GIT] Initializing repository...
  git init || goto :fail
)

git branch -M main || goto :fail

rem Add or repair the origin remote without creating duplicates.
git remote get-url origin >nul 2>&1
if errorlevel 1 (
  git remote add origin "%REMOTE%" || goto :fail
) else (
  git remote set-url origin "%REMOTE%" || goto :fail
)

echo [GIT] Staging source files...
git add . || goto :fail

rem Extra secret check after staging.
git diff --cached --name-only | findstr /R /I /X ".env" >nul
if not errorlevel 1 (
  echo [ERROR] .env unexpectedly entered the Git index. Cancelling push.
  git reset -- .env >nul 2>&1
  pause
  exit /b 1
)

rem Never push generated release/runtime directories even if an old Git index tracked them.
for %%P in (compile compiled release releases) do (
  git rm -r --cached --ignore-unmatch "%%P" >nul 2>&1
)

echo.
echo [CHECK] Files currently staged:
git status --short
echo.
set "CONFIRM="
set /p "CONFIRM=Type PUSH to commit and push these files: "
if /I not "%CONFIRM%"=="PUSH" (
  echo [CANCELLED] Nothing was pushed. Staged changes were left intact for review.
  pause
  exit /b 0
)

set "MSG="
set /p "MSG=Commit message [Update AITS]: "
if "%MSG%"=="" set "MSG=Update AITS"

git diff --cached --quiet
if errorlevel 1 (
  git commit -m "%MSG%" || goto :fail
) else (
  echo [INFO] No new staged changes to commit.
)

echo [GIT] Pushing main...
git push -u origin main
if errorlevel 1 (
  echo.
  echo [ERROR] Push failed. If GitHub requests authentication, sign in with Git Credential Manager or use a Personal Access Token.
  pause
  exit /b 1
)

echo.
echo [OK] AITS source pushed successfully.
pause
exit /b 0

:fail
echo.
echo [ERROR] Git operation failed. No project source/data was deleted.
pause
exit /b 1
