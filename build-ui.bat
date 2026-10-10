@echo off
setlocal
cd /d "%~dp0"

echo [1/4] Installing frontend dependencies...
pushd frontend
call npm install
if errorlevel 1 ( echo npm install failed & popd & pause & exit /b 1 )

echo [2/4] Building the Vue app...
call npm run build
if errorlevel 1 ( echo vite build failed & popd & pause & exit /b 1 )
popd

echo [3/4] Embedding the UI...
if exist internal\web\dist rmdir /s /q internal\web\dist
mkdir internal\web\dist
xcopy /e /i /y frontend\dist internal\web\dist >nul

echo [4/4] Building the Go binary...
go build -o tally-web-ui.exe .
if errorlevel 1 ( echo go build failed & pause & exit /b 1 )

echo.
echo Done. Run run.bat or: tally-web-ui.exe
endlocal
