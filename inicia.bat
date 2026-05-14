@echo off
setlocal
cd /d "%~dp0"
echo Atualizando o pacote github.com/Nyllson-N/gocker...
go get -u github.com/Nyllson-N/gocker
if errorlevel 1 (
    echo Erro ao atualizar o pacote.
    pause
    exit /b 1
)
echo Atualizando dependencias com go mod tidy...
go mod tidy
if errorlevel 1 (
    echo Erro em go mod tidy.
    pause
    exit /b 1
)
echo Iniciando Api/main.go...
go run .\Api\main.go
if errorlevel 1 (
    echo Erro ao iniciar Api/main.go.
    pause
    exit /b 1
)
echo Programa finalizado com sucesso.
pause
