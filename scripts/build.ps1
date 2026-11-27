#Requires -Version 7.3
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$repo = Split-Path -Parent $PSScriptRoot
$release = Join-Path $repo 'release'
New-Item -ItemType Directory -Path $release -Force | Out-Null

Push-Location (Join-Path $repo 'backend')
try {
    go build -trimpath -o (Join-Path $release 'guardian.exe') ./cmd/guardian
} finally {
    Pop-Location
}
Push-Location (Join-Path $repo 'extension')
try {
    npm ci
    npm run check
    npm run build
} finally {
    Pop-Location
}
Write-Host 'Built release/guardian.exe and extension/dist.'
