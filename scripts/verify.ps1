param([switch]$Build)
$ErrorActionPreference = 'Stop'
$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $projectRoot
try {
    Push-Location (Join-Path $projectRoot 'frontend')
    try {
        & npm.cmd ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
        & npm.cmd test
        if ($LASTEXITCODE -ne 0) { throw 'frontend tests failed' }
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw 'frontend build failed' }
    } finally { Pop-Location }
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
    if ($Build) {
        $previousBin = $env:GOBIN
        $toolDirectory = Join-Path $env:TEMP 'res-downloader-build-tools'
        try {
            $env:GOBIN = $toolDirectory
            & go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
            if ($LASTEXITCODE -ne 0) { throw 'Wails installation failed' }
        } finally { $env:GOBIN = $previousBin }
        & (Join-Path $toolDirectory 'wails.exe') build -skipbindings -s
        if ($LASTEXITCODE -ne 0) { throw 'Wails build failed' }
    }
    Write-Host 'Verification passed.'
} finally { Pop-Location }
