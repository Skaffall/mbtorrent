# Сборка mbtorrent под Windows (PowerShell 5+ или 7+).
# Нужен только Go 1.22+: https://go.dev/dl/
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$version = (Select-String -Path 'internal\buildinfo\buildinfo.go' -Pattern 'var Version = "(.*)"').Matches[0].Groups[1].Value

New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = '0'

Write-Host "→ сборка mbtorrent $version" -ForegroundColor Cyan
go build -trimpath -buildvcs=false `
  -ldflags "-s -w -H=windowsgui -X mbtorrent/internal/buildinfo.Version=$version" `
  -o dist\mbtorrent.exe .

if ($LASTEXITCODE -ne 0) { throw "Сборка не удалась" }

$size = [math]::Round((Get-Item dist\mbtorrent.exe).Length / 1MB, 1)
Write-Host "Готово: dist\mbtorrent.exe ($size МБ)" -ForegroundColor Green
Write-Host "Можно скопировать exe куда угодно — зависимостей нет."
