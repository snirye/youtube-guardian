#Requires -Version 7.3
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
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
