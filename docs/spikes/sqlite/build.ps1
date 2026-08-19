# Assertion 7: CGO_ENABLED=0 cross-compile for windows/amd64 and
# linux/amd64 from one machine. Run from the module root:
#   .\docs\spikes\sqlite\build.ps1
$ErrorActionPreference = "Stop"
Set-Location "$PSScriptRoot\..\..\.."

$out = "$env:TEMP\sqlite-xcompile"
New-Item -ItemType Directory -Force -Path $out | Out-Null

Write-Host "--- windows/amd64, CGO_ENABLED=0 ---"
$env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -o "$out\spike-windows.exe" ./docs/spikes/sqlite

Write-Host "--- linux/amd64, CGO_ENABLED=0 ---"
$env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -o "$out\spike-linux" ./docs/spikes/sqlite

Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED

Write-Host "Built: $out\spike-windows.exe and $out\spike-linux"
Write-Host "Run each on its native platform to confirm assertions 1-6 also pass there."
