@echo off
setlocal
cd /d "%~dp0"
if not exist "tally-web-ui.exe" (
  echo Building tally-web-ui...
  go build -o tally-web-ui.exe .
  if errorlevel 1 (
    echo Build failed.
    pause
    exit /b 1
  )
)
tally-web-ui.exe %*
endlocal
