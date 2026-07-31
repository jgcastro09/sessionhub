@echo off
title NodeStage Task Board - Liberar Porta 8090 no Firewall
echo ==================================================
echo Liberando porta 8090 no Firewall do Windows...
echo ==================================================
echo.

netsh advfirewall firewall add rule name="NodeStage Task Board (Port 8090)" dir=in action=allow protocol=TCP localport=8090

echo.
echo Se apareceu "Ok.", a porta 8090 foi liberada com sucesso!
pause
