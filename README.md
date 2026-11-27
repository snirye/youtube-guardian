# YouTube Guardian

YouTube Guardian pairs a Chrome MV3 extension with a local Windows service. The extension lets the child browse YouTube, then checks the current video's channel before playback. A private Telegram bot manages channel approvals. The backend stores settings and activity in SQLite and uses an outbound Telegram connection.

**[Download YouTube Guardian for Windows x64](https://github.com/snirye/youtube-guardian/releases)**

Download `YouTubeGuardianSetup.exe` from the release assets and open it to install the Windows service, pair Telegram, and check that the Chrome extension has connected.

- [Development builds from GitHub Actions](https://github.com/snirye/youtube-guardian/actions/workflows/build-windows.yml)
- [Windows installation and removal](docs/WINDOWS.md)
- [Browser tests and enforcement limits](docs/VERIFICATION.md)
- [Implementation plan](docs/IMPLEMENTATION_PLAN.md)
- [Development milestones](docs/DEVELOPMENT.md)

## What the child sees

A video from an unknown channel stays blocked and shows a Hebrew approval button. An explicit request sends a message to the parent. Approving it updates Chrome without restarting the browser. Blocked channels cannot generate another request. Regular blocked visits are recorded locally.

The default setting also hides supported Shorts surfaces and blocks direct Shorts URLs. The parent can change Shorts filtering with `/shorts_on` and `/shorts_off`. Turning off Shorts filtering still requires a channel approval.

## Development

Use Go 1.25.8 or newer and Node.js 20 or newer. Build the extension from the repository root.

```sh
npm --prefix extension ci
npm --prefix extension run check
npm --prefix extension run build
```

For a foreground backend, create a private `guardian.json` with the fields below. Generate the API token with `openssl rand -hex 32` or a system cryptographic random generator. If you omit both Telegram settings, the backend starts with an empty approval list and without a bot connection.

```json
{
  "api_token": "REPLACE_WITH_A_RANDOM_64_CHARACTER_HEX_TOKEN",
  "port": 8765,
  "database_path": "guardian.sqlite",
  "retention_days": 30
}
```

The default database path is relative to `guardian.json`. The backend creates the SQLite database and applies embedded migrations at startup. Run the service in the foreground from `backend/`.

```sh
go run ./cmd/guardian -config /path/to/guardian.json
```

Load `extension/dist` through **Load unpacked** in `chrome://extensions`, then enter the API token and port in the extension options. The token authenticates access to `http://127.0.0.1`. An authenticated `GET /health` returns HTTP 200 with `{"status":"ok"}` when the database is healthy.

Run `go test ./...` and `go vet ./...` from `backend/`. See [verification](docs/VERIFICATION.md) for the automated Chrome suite and the manual checklist for a Windows deployment.
