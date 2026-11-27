#Requires -Version 7.3
#Requires -RunAsAdministrator
param(
    [ValidateRange(1, 65535)][int]$Port = 8765,
    [ValidateRange(1, 3650)][int]$RetentionDays = 30,
    [string]$ParentTelegramUserId,
    [SecureString]$TelegramBotToken,
    [switch]$CreateManagedTelegramBot,
    [SecureString]$TelegramManagerBotToken,
    [switch]$ReuseConfiguration
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$PSNativeCommandArgumentPassing = 'Standard'
$service = 'YouTubeGuardian'
$extensionId = 'jcpbnaipmnmgfoelllffgphnfbbngdik'
$repo = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $repo 'release/guardian.exe'
$extension = Join-Path $repo 'extension/dist'
$program = Join-Path $env:ProgramFiles $service
$data = Join-Path $env:ProgramData $service
$configPath = Join-Path $data 'guardian.json'
$policy = "HKLM:\SOFTWARE\Policies\Google\Chrome\3rdparty\extensions\$extensionId\policy"
$admins = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')
$system = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-18')
$localService = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-19')
$installer = [System.Security.Principal.WindowsIdentity]::GetCurrent().User

if ($CreateManagedTelegramBot -and ($ReuseConfiguration -or $TelegramBotToken -or $ParentTelegramUserId)) {
    throw 'Managed mode requires a new installation and a dedicated manager bot.'
}
if ($TelegramManagerBotToken -and !$CreateManagedTelegramBot) {
    throw 'Use -CreateManagedTelegramBot with a manager bot token.'
}
if (Get-Service $service -ErrorAction SilentlyContinue) {
    throw 'The YouTube Guardian service already exists. Run uninstall.ps1 before reinstalling.'
}
if (!(Test-Path -LiteralPath $binary -PathType Leaf) -or !(Test-Path -LiteralPath (Join-Path $extension 'manifest.json'))) {
    throw 'Extract the complete Windows package, or run scripts/build.ps1 from the source checkout.'
}
if ((Test-Path -LiteralPath $data) -ne $ReuseConfiguration.IsPresent) {
    throw 'Use -ReuseConfiguration when reinstalling with existing service data. A new install needs an unused data directory.'
}

function Assert-PlainTrustedItem([string]$Path) {
    $item = Get-Item -LiteralPath $Path -Force
    if ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
        throw "Unexpected reparse point at $Path."
    }
    $identity = (Get-Acl -LiteralPath $Path).GetOwner([System.Security.Principal.SecurityIdentifier])
    if ($identity -notin @($admins, $system, $localService, $installer)) {
        throw "Unexpected owner for $Path."
    }
}

$acl = [System.Security.AccessControl.DirectorySecurity]::new()
$acl.SetOwner($admins)
$acl.SetAccessRuleProtection($true, $false)
$inherit = [System.Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
foreach ($identity in @($admins, $system, $localService)) {
    $rights = if ($identity -eq $localService) { 'Modify' } else { 'FullControl' }
    $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
        $identity, $rights, $inherit, 'None', 'Allow'
    )
    $acl.AddAccessRule($rule)
}

if ($ReuseConfiguration) {
    Assert-PlainTrustedItem $data
    $current = Get-Acl -LiteralPath $data
    $allowed = @($admins.Value, $system.Value, $localService.Value)
    foreach ($rule in $current.GetAccessRules($true, $true, [System.Security.Principal.SecurityIdentifier])) {
        if ($rule.AccessControlType -eq 'Allow' -and $rule.IdentityReference.Value -notin $allowed) {
            throw 'The existing data directory grants access to an unexpected principal.'
        }
    }
    foreach ($file in Get-ChildItem -LiteralPath $data -Force) {
        Assert-PlainTrustedItem $file.FullName
        foreach ($rule in (Get-Acl -LiteralPath $file.FullName).GetAccessRules($true, $true, [System.Security.Principal.SecurityIdentifier])) {
            if ($rule.AccessControlType -eq 'Allow' -and $rule.IdentityReference.Value -notin $allowed) {
                throw "Unexpected access rules on $($file.Name)."
            }
        }
    }
    $settings = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json -AsHashtable
    $Port = $settings['port'] ?? 8765
    $apiToken = $settings['api_token']
    if ($settings['database_path'] -and $settings['database_path'] -ne 'guardian.sqlite') {
        throw 'Service configuration must keep guardian.sqlite in the protected service directory.'
    }
    if ($Port -isnot [ValueType] -or $Port -lt 1 -or $Port -gt 65535 -or $apiToken -notmatch '^[a-fA-F0-9]{64}$') {
        throw 'The existing configuration has an invalid API token or port.'
    }
} else {
    $telegramSetup = $null
    if ($CreateManagedTelegramBot) {
        if (!$TelegramManagerBotToken) {
            $TelegramManagerBotToken = Read-Host 'Telegram manager bot token' -AsSecureString
        }
        $managerToken = [System.Net.NetworkCredential]::new('', $TelegramManagerBotToken).Password
        try {
            $telegramSetup = ($managerToken | & $binary -telegram-setup managed) | ConvertFrom-Json -AsHashtable
        } finally {
            $managerToken = $null
        }
        $botToken = $telegramSetup['bot_token']
    } else {
        if (!$TelegramBotToken) { $TelegramBotToken = Read-Host 'BotFather token' -AsSecureString }
        $botToken = [System.Net.NetworkCredential]::new('', $TelegramBotToken).Password
        if (!$ParentTelegramUserId) {
            $telegramSetup = ($botToken | & $binary -telegram-setup pair) | ConvertFrom-Json -AsHashtable
        }
    }

    $parentId = 0L
    if ($telegramSetup) {
        $parentId = [long]$telegramSetup['parent_id']
        $account = if ($telegramSetup['parent_username']) { '@' + $telegramSetup['parent_username'] } else { 'Telegram user' }
        Write-Host "Detected $account (ID $parentId) for @$($telegramSetup['bot_username'])."
        if ((Read-Host 'Use this account for approvals? [y/N]') -notmatch '^(y|yes)$') {
            throw 'Telegram setup canceled.'
        }
    } elseif (![long]::TryParse($ParentTelegramUserId, [ref]$parentId)) {
        throw 'Enter a positive numeric Telegram user ID.'
    }
    if ($parentId -le 0) { throw 'Enter a positive numeric Telegram user ID.' }
    if ($botToken -notmatch '^[0-9]+:[A-Za-z0-9_-]{15,}$') { throw 'The Telegram bot token format is invalid.' }
    $apiToken = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
    [System.IO.FileSystemAclExtensions]::Create([System.IO.DirectoryInfo]::new($data), $acl)
    @{
        port = $Port
        api_token = $apiToken
        telegram_bot_token = $botToken
        parent_telegram_user_id = $parentId
        retention_days = $RetentionDays
    } | ConvertTo-Json | Set-Content -LiteralPath $configPath -Encoding utf8
    $botToken = $null
    if ($telegramSetup) {
        Write-Host "Open https://t.me/$($telegramSetup['bot_username']) and tap Start in Telegram if prompted."
        $telegramSetup = $null
    }
}

if (Test-Path -LiteralPath $program) {
    if ((Get-Item -LiteralPath $program).Attributes -band [System.IO.FileAttributes]::ReparsePoint) {
        throw 'Unexpected reparse point in the installation directory.'
    }
}
New-Item -ItemType Directory -Path $program -Force | Out-Null
$installedExtension = Join-Path $program 'extension'
New-Item -ItemType Directory -Path $installedExtension -Force | Out-Null
Copy-Item -LiteralPath $binary -Destination (Join-Path $program 'guardian.exe') -Force
Get-ChildItem -LiteralPath $extension -File | Copy-Item -Destination $installedExtension -Force
New-Item -Path $policy -Force | Out-Null
New-ItemProperty -Path $policy -Name apiToken -Value $apiToken -PropertyType String -Force | Out-Null
New-ItemProperty -Path $policy -Name port -Value $Port -PropertyType DWord -Force | Out-Null

if (![System.Diagnostics.EventLog]::SourceExists($service)) {
    [System.Diagnostics.EventLog]::CreateEventSource($service, 'Application')
}
$command = '"{0}" -config "{1}"' -f (Join-Path $program 'guardian.exe'), $configPath
sc.exe create $service 'binPath=' $command 'start=' 'auto' 'obj=' 'NT AUTHORITY\LocalService' 'DisplayName=' 'YouTube Guardian'
try {
    sc.exe description $service 'Local backend for YouTube Guardian'
    sc.exe failure $service 'reset=' 86400 'actions=' 'restart/5000/restart/10000/restart/30000'
    sc.exe failureflag $service 1
    Start-Service $service

    $health = Invoke-RestMethod "http://127.0.0.1:${Port}/health" -Headers @{ Authorization = "Bearer $apiToken" } -TimeoutSec 5 -NoProxy
    if ($health.status -ne 'ok') { throw 'The local health check failed.' }
} catch {
    Stop-Service $service -ErrorAction SilentlyContinue
    sc.exe delete $service
    throw
}

Write-Host "Load the unpacked Chrome extension from $installedExtension in the child's Chrome profile."
Write-Host 'Reload Chrome policies at chrome://policy, then check /status in Telegram.'
