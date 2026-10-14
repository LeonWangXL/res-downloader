@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\verify.ps1" -Build
if errorlevel 1 exit /b 1
