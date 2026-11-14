@echo off
rem Double-clickable entry for build.ps1: Windows blocks .ps1 by execution
rem policy, and this wrapper passes the action through without changing the
rem policy for the machine.
rem
rem   build.cmd            -> the development build
rem   build.cmd check      -> gofmt / build / vet / test
rem   build.cmd release    -> stripped production binary in dist\
rem   build.cmd smoke      -> launch, confirm it stays up, kill

setlocal
set "ACTION=%~1"
if "%ACTION%"=="" set "ACTION=build"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %ACTION% %2 %3 %4 %5
exit /b %ERRORLEVEL%
