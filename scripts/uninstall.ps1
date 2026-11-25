#Requires -Version 7.3
#Requires -RunAsAdministrator
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$service = 'YouTubeGuardian'
$extensionId = 'jcpbnaipmnmgfoelllffgphnfbbngdik'
$policy = "HKLM:\SOFTWARE\Policies\Google\Chrome\3rdparty\extensions\$extensionId\policy"

if (Get-Service $service -ErrorAction SilentlyContinue) {
    Stop-Service $service
    sc.exe delete $service
}
if (Test-Path $policy) {
    Remove-ItemProperty -Path $policy -Name apiToken, port -ErrorAction SilentlyContinue
}
if ([System.Diagnostics.EventLog]::SourceExists($service)) {
    [System.Diagnostics.EventLog]::DeleteEventSource($service)
}
Write-Host 'Remove YouTube Guardian from chrome://extensions in each profile where it was loaded.'
Write-Host "Service data is retained at $(Join-Path $env:ProgramData $service)."
Write-Host "The program directory can be removed after removing the extension: $(Join-Path $env:ProgramFiles $service)."
