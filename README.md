# YouTube Guardian

YouTube Guardian is being developed as a local parental-control system for a shared Windows computer. The planned Chrome extension will allow playback only from approved YouTube channels. Parents will manage approvals through Telegram.

The foundation provides a standalone Go backend, a SQLite schema, and an authenticated health endpoint. Browser enforcement and Telegram integration are upcoming milestones.

- [Implementation plan](docs/IMPLEMENTATION_PLAN.md)
- [Development milestones](docs/DEVELOPMENT.md)
- [Pre-playback feasibility probe](tests/playback-probe/README.md)

## Run the backend locally

Use Go 1.25.8 or newer. SQLite is embedded in the binary, so a separate SQLite installation or C compiler is unnecessary.

Create a `guardian.json` file in a private directory. Generate a random 32-byte token and encode it as 64 hexadecimal characters. The path below is relative to `guardian.json`, and SQLite creates the database on first startup. An explicitly configured database directory must already exist.

```json
{
  "port": 8765,
  "database_path": "guardian.sqlite",
  "api_token": "REPLACE_WITH_A_RANDOM_64_CHARACTER_HEX_TOKEN"
}
```

From the repository root in PowerShell 7 on Windows, the following creates a config in the current account's local application data directory if one is absent and starts the backend in the foreground.

```powershell
$guardianDir = Join-Path $env:LOCALAPPDATA 'YouTubeGuardian'
$guardianConfig = Join-Path $guardianDir 'guardian.json'
New-Item -ItemType Directory -Force $guardianDir | Out-Null
if (-not (Test-Path $guardianConfig)) {
    $token = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
    @{ api_token = $token } | ConvertTo-Json | Set-Content -Encoding utf8 $guardianConfig
}
Set-Location backend
go run ./cmd/guardian -config $guardianConfig
```

From another PowerShell window, check the running backend.

```powershell
$path = Join-Path $env:LOCALAPPDATA 'YouTubeGuardian/guardian.json'
$cfg = Get-Content -Raw $path | ConvertFrom-Json
$port = $cfg.port ?? 8765
Invoke-RestMethod "http://127.0.0.1:${port}/health" -Headers @{ Authorization = "Bearer $($cfg.api_token)" }
```

On Linux or macOS, generate a token with `openssl rand -hex 32` and run `go run ./cmd/guardian -config /path/to/guardian.json` from `backend/`. Send `GET /health` with the token in the `Authorization: Bearer <token>` header. A healthy backend returns HTTP 200 with `{"status":"ok"}`. Missing or invalid credentials return 401, and a failed database check returns 503. The backend listens on `127.0.0.1`. Press Ctrl+C to stop it.

## Develop and test

Run `go test ./...` and `go vet ./...` from `backend/`. The initial [migration](backend/migrations/001_foundation.sql) creates explicit channel decisions, approval requests, activity events, and a configuration version starting at 1. An unknown channel has no row in `channels`. SQLite enforces one pending request per channel and runs migrations when the backend opens the database.
