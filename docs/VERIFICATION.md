# Verification

## Run automated checks

Use Go 1.25.8+, Node.js 20+, and OpenSSL on Linux. The browser test compiles a Go test binary, builds a temporary local fixture with the YouTube and media hostnames, and loads the actual MV3 extension into Playwright's Chrome for Testing. The Chrome profile routes those hostnames to loopback. Telegram calls in the Go test binary go to a local HTTP test double.

```sh
cd backend
go test ./...
go vet ./...
cd ..
npm --prefix extension ci
npm --prefix extension run check
npm --prefix extension run build
cd tests/e2e
npm ci
npx playwright install chromium
npm test
```

To record the same test, run `GUARDIAN_RECORD_DIR=/absolute/output/path npm test` from `tests/e2e`. The script writes a WebM capture and the path to the selected recording.

The suite checks zero frames and zero delivered media requests for an unknown channel; explicit approval, duplicate requests, and invalid or stale Telegram callbacks; approved playback; navigation between videos in one document; revocation; recent logs and heartbeat; backend outage and recovery with persistent permissions; and independently toggled Shorts blocking. The [initial feasibility probe](../tests/playback-probe/README.md) also compares an early pause handler with an MV3 network rule.

## Manual verification on Windows

1. Install the service and extension with [the Windows guide](WINDOWS.md). Restart Windows and verify that the service is running before Chrome opens.
2. Open a regular YouTube watch page in the child's profile. Verify that an unapproved video stays silent and hidden during initial load. Browse the home page and search results.
3. Request approval once, click **Approve** in the parent's real Telegram chat, and verify that the video plays after the next configuration check. Change a previously allowed channel to blocked with `/block` and confirm that playback stops.
4. Exercise sidebar navigation, browser Back/Forward, playlist next, autoplay, Shorts, and full-page embeds. Use ordinary and signed-in sessions. Test regular and adaptive streams, and investigate any approved video that stays blocked.
5. Stop the Windows service while a video is playing. Verify that the overlay appears and playback stops, then start the service and verify recovery. Confirm that the request and logs survive restart and inspect `/status`.
6. Check from the standard Windows account that the service files and SQLite data under ProgramData are inaccessible. Verify removal, then reinstall with the retained data.

## Current evidence and limits

Chrome for Testing on Linux verified the end-to-end workflow on the deterministic fixture. Live YouTube was unreachable from the development environment. Windows amd64 cross-compilation and PowerShell parsing were checked on Linux. The Windows Service Control Manager, Windows ACLs, and a live Telegram bot still need the manual checks above.

A static Chrome rule denies matching media from Google Video on YouTube. An allow rule is scoped to the current tab and the media IDs read from the current video's player response. The page scripts suppress autoplay, pause and mute the player, and hide media without a recent verified grant. Background refresh uses fresh snapshots even after a service worker restart. When the service is unreachable, the extension revokes permission on the next failed check or when the current short lease expires. Background tab scheduling can affect timing.

A site change can make channel metadata or the playable media ID unavailable. In that case the extension keeps the video blocked, including on approved channels. Live streams, ads, DRM paths, adaptive streaming, and cached or blob data held by YouTube need real-player verification. A network rule can stop matching requests but cannot erase browser-held data. An early pause handler alone cannot guarantee that a decoder never produced a frame. The extension's visual and audible suppression adds a separate control while authorization is unresolved.

The extension supports desktop and mobile YouTube watch URLs, watch URLs opened from playlists, Shorts URLs, and top-level embed URLs when the current player supplies verifiable metadata and media IDs. Iframe embeds remain blocked. On browse pages, animated video previews can be suppressed while thumbnails and search navigation remain available. Shorts visibility relies on YouTube's current element markup.
