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
    # Bundled plugins go in dist\plugins, which the server reads by default.
    foreach ($p in @("watchlist-rss")) {
        New-Item -ItemType Directory -Force "$PSScriptRoot\dist\plugins\$p" | Out-Null
        go build -trimpath -ldflags "-s -w" -o "$PSScriptRoot\dist\plugins\$p\$p.exe" "./plugins/$p"
        Copy-Item "plugins\$p\plugin.json" "$PSScriptRoot\dist\plugins\$p\"
    }
} finally { Pop-Location }

Write-Host "Built dist\bingebandit.exe ($Version)"
