# Development milestones

## 1. Foundation

1. Initialize the Go module and the backend entry point. Load and validate configuration at startup, resolve the database path relative to the configuration file, and keep the HTTP listener on IPv4 loopback.
2. Embed and apply transactional SQLite migrations. Create tables for explicit channel decisions, approval requests, activity events, and the configuration version. Represent unknown channels by the absence of a decision and enforce one pending request per channel.
3. Expose an authenticated health endpoint that checks the database. Wire startup failures, HTTP timeouts, and graceful shutdown into the entry point.
4. Test configuration validation, fresh and repeated migrations, the database constraints, and health responses. Document local setup and the schema.
5. Run a bounded Manifest V3 playback experiment in Chromium before implementing the extension. Exercise an autoplaying fixture with an early pause handler and with network requests blocked before the page loads. Use the result to define the scope of the browser tests in milestone 2.

## 2. YouTube enforcement

1. Add the MV3 extension and bind channel identification and permission checks to the current video across YouTube SPA navigation.
2. Add the Hebrew blocking overlay and explicit approval request action. Keep playback blocked while identification or authorization is pending.
3. Exercise first load, SPA navigation races, autoplay, playlists, Shorts, and supported embeds. Measure first-frame behavior on real YouTube and document limits by entry point.

## 3. Telegram management

1. Implement parent authorization, long polling, and command registration.
2. Add channel management and the approval workflow. Make request resolution transactional and reject stale callbacks. Test command and callback handling.

## 4. Synchronization and logs

1. Serve versioned permissions and refresh the extension cache. Test revocation and loss of backend connectivity.
2. Store playback decisions and approval activity and expose recent events through Telegram. Apply the configured retention.

## 5. Monitoring and reliability

1. Report extension heartbeats and classify recent activity in Telegram.
2. Test recovery across MV3 worker suspension, backend restart, and connectivity failures.

## 6. Windows deployment

1. Add service installation and removal scripts, startup and recovery configuration, and filesystem permissions.
2. Verify the installation with separate standard and administrator accounts and record deployment limitations.

## 7. End-to-end testing

1. Verify the browser, backend, and Telegram flows together, including approval, revocation, and restart.
2. Finish the installation guide and record remaining manual coverage and known limitations.

## 8. Future Shorts filtering

Hide Shorts surfaces and block direct Shorts navigation independently of channel decisions once the MVP is stable.
