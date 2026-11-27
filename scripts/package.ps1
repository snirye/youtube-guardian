#Requires -Version 7.3
param([switch]$TestInstaller)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
$setupSource = Join-Path $repo 'backend/cmd/setup'
$setupExe = Join-Path $repo 'release/YouTubeGuardianSetup.exe'
$archive = Join-Path $repo 'release/youtube-guardian-windows-x64.zip'
$binary = Join-Path $repo 'release/guardian.exe'
$extension = Join-Path $repo 'extension/dist'
$extensionFiles = @(
    'manifest.json', 'rules.json', 'managed.json', 'guardian.css',
    'background.js', 'content.js', 'page.js', 'options.js', 'options.html', 'shorts.html'
)
if (!(Test-Path -LiteralPath $binary -PathType Leaf)) {
    throw 'Build release/guardian.exe for Windows x64 before packaging.'
}

$staging = [System.IO.Directory]::CreateTempSubdirectory('guardian-package-').FullName
try {
    foreach ($directory in @('release', 'extension/dist', 'scripts', 'docs', 'tests/playback-probe')) {
        New-Item -ItemType Directory -Path (Join-Path $staging $directory) | Out-Null
    }

    Copy-Item -LiteralPath $binary -Destination (Join-Path $staging 'release')
    foreach ($file in $extensionFiles) {
        Copy-Item -LiteralPath (Join-Path $extension $file) -Destination (Join-Path $staging 'extension/dist')
    }
    $files = @(
        'scripts/install.ps1', 'scripts/uninstall.ps1', 'docs/WINDOWS.md',
        'docs/VERIFICATION.md', 'tests/playback-probe/README.md'
    )
    foreach ($file in $files) {
        Copy-Item -LiteralPath (Join-Path $repo $file) -Destination (Join-Path $staging $file)
    }

    Compress-Archive -Path (Join-Path $staging '*') -DestinationPath $archive -Force
} finally {
    Remove-Item -LiteralPath $staging -Recurse -Force
}
Write-Host "Created $archive"


# Embed the already-built service and extension in the Windows setup executable.
$PSNativeCommandUseErrorActionPreference = $true
$setupStaging = [System.IO.Directory]::CreateTempSubdirectory('guardian-setup-').FullName
$payload = Join-Path $setupSource 'payload.zip'
$resource = Join-Path $setupSource 'manifest_windows_amd64.syso'
try {
    $setupExtension = Join-Path $setupStaging 'extension'
    New-Item -ItemType Directory -Path $setupExtension | Out-Null
    Copy-Item -LiteralPath $binary -Destination $setupStaging
    foreach ($file in $extensionFiles) {
        Copy-Item -LiteralPath (Join-Path $extension $file) -Destination $setupExtension
    }
    Compress-Archive -Path (Join-Path $setupStaging '*') -DestinationPath $payload -Force

    Push-Location (Join-Path $repo 'backend')
    $previousOS, $previousArch, $previousCgo = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
    try {
        $env:GOOS = go env GOHOSTOS
        $env:GOARCH = go env GOHOSTARCH
        go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest (Join-Path $setupSource 'setup.manifest') -o $resource
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        go build -tags guardian_setup -trimpath -ldflags '-H windowsgui' -o $setupExe ./cmd/setup
        if ($TestInstaller) { go test -tags guardian_setup -v ./cmd/setup }
    } finally {
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $previousOS, $previousArch, $previousCgo
        Pop-Location
    }
} finally {
    Remove-Item -LiteralPath $setupStaging -Recurse -Force
    foreach ($file in @($payload, $resource)) {
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file -Force }
    }
}
Write-Host "Created $setupExe"
