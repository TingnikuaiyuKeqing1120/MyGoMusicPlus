@echo off
powershell.exe -NoProfile -ExecutionPolicy RemoteSigned -File "%~dp0import-profile.ps1" -Interactive
if errorlevel 1 pause
