# Windows installation

Run on Windows 10 or 11 with a separate administrator account and a standard Windows account for the child. The prebuilt Windows x64 package requires PowerShell 7.3 or newer and Chrome 120 or newer.

## Download the Windows x64 package

1. Sign in to GitHub and open the [Build Windows package workflow](https://github.com/snirye/youtube-guardian/actions/workflows/build-windows.yml). Open a successful run. Artifacts stay available for 30 days to users with access to the repository.
2. In the run summary under **Artifacts**, download `youtube-guardian-windows-x64.zip`. It contains the compiled service, Chrome extension, installation scripts, and this guide.
3. Unblock and extract the downloaded ZIP into a local folder. This lets PowerShell run the extracted scripts with the process-level `RemoteSigned` policy.

```powershell
Unblock-File .\youtube-guardian-windows-x64.zip
Expand-Archive .\youtube-guardian-windows-x64.zip .\youtube-guardian
Set-Location .\youtube-guardian
```

To build from source instead, install Git, Go 1.25.8 or newer, and Node.js 20 or newer. Run `./scripts/build.ps1` from the repository root. Run `./scripts/package.ps1` if you also need a ZIP.

## Prepare Telegram

1. Create a dedicated bot with Telegram's [BotFather](https://t.me/BotFather). Save the bot token privately. Open the bot from the parent's account and send `/start`.
2. Find the numeric ID of the parent's Telegram account. The following PowerShell calls Telegram for the bot's unread private messages. Run it before installing the service, since the service will also poll the bot. If the chat is empty, send the bot another message from the parent's account and try again.

```powershell
$secure = Read-Host 'BotFather token' -AsSecureString
$token = [System.Net.NetworkCredential]::new('', $secure).Password
try {
    (Invoke-RestMethod "https://api.telegram.org/bot$token/getUpdates").result |
        Where-Object { $_.message.chat.type -eq 'private' -and -not $_.message.from.is_bot } |
        ForEach-Object { $_.message.from | Select-Object id, first_name, username } |
        Sort-Object id -Unique
} finally {
    $token = $null
}
```

Only the configured user ID can manage channels, and commands and buttons are accepted only in that user's private chat with the bot. Keep the bot token in the backend configuration.

## Install the service and load Chrome

1. Open PowerShell 7 **as administrator** in the extracted package folder or the built repository root. Run `pwsh -NoProfile -ExecutionPolicy RemoteSigned -File ./scripts/install.ps1`. Enter the parent's Telegram ID and the bot token when prompted. The installer validates the local API and registers `YouTubeGuardian` for automatic startup and recovery.
2. Sign in to the child's standard Windows account. In Chrome, open `chrome://extensions`, turn on **Developer mode**, and use **Load unpacked** to select the `YouTubeGuardian\extension` folder under Program Files. The installer prints the exact path. In `chrome://policy`, select **Reload policies** and check that the extension receives its managed settings. Restart Chrome or reload existing YouTube tabs after first installation.
3. Send `/status` to the bot. With Chrome open, allow roughly 30 seconds for a heartbeat. Then use the [manual verification checklist](VERIFICATION.md).

The service runs as LocalService. The configuration and SQLite database are under `%ProgramData%\YouTubeGuardian` with access restricted to administrators, SYSTEM, and LocalService. The Chrome policy distributes only the local API token and port. The service accepts local API requests on IPv4 loopback and makes outbound HTTPS requests to Telegram, so it needs no router port forwarding.

Windows Event Viewer records service output under **Windows Logs → Application → YouTubeGuardian**. Use `Get-Service YouTubeGuardian` to inspect the service. If Telegram cannot reach the bot, check service health in Windows. A bot that receives no updates cannot report a backend outage from the computer.

## Parent commands

| Command | Effect |
| --- | --- |
| `/status` | Backend and most recent extension heartbeat |
| `/pending` | Pending approval requests |
| `/allow UC… [name]` | Approve a channel using its stable ID |
| `/block UC… [name]` | Block a channel or revoke approval |
| `/channels` | Approved channels |
| `/logs` | Recent activity |
| `/shorts_on` | Hide supported Shorts surfaces and block Shorts URLs |
| `/shorts_off` | Apply normal channel rules to Shorts |
| `/help` | All commands |

A request notification includes the channel, video, time and Approve/Reject buttons. Reject marks the channel blocked. To reverse it, use `/allow` in the parent's chat.

## Remove or reinstall

From an elevated PowerShell 7 session run `./scripts/uninstall.ps1`. Remove the extension through `chrome://extensions` in each installed profile. The script retains the database and configuration. To reinstall with these, extract a new package or rebuild from source, then run `./scripts/install.ps1 -ReuseConfiguration`. After removing the extension, administrators can delete the installed program directory if desired.

## Browser enforcement limits

Loading the extension unpacked is sufficient for testing. On a consumer Windows PC, users can remove or disable an unpacked extension and use other browsers or profiles. Chrome usually disables extensions in Incognito until explicitly allowed. For a managed deployment, use your organization's Chrome extension distribution policies and restrict unapproved browsers and profiles with Windows account controls. Administrators can change or remove the Windows controls. See [verification](VERIFICATION.md) for the video paths the project can verify.
