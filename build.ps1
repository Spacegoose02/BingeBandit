# Builds the web UI and the server into dist\bingebandit.exe (Windows).
# Usage: .\build.ps1 [-Version v0.1.0]
param([string]$Version = "dev")
$ErrorActionPreference = "Stop"

Push-Location "$PSScriptRoot\web"
try {
    if (-not (Test-Path node_modules)) { npm ci }
    npm run build
} finally { Pop-Location }

Push-Location "$PSScriptRoot\server"
try {
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o "$PSScriptRoot\dist\bingebandit.exe" ./cmd/bingebandit
} finally { Pop-Location }

Write-Host "Built dist\bingebandit.exe ($Version)"
