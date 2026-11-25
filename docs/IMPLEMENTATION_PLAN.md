# YouTube Guardian — Implementation Plan

## 1. Goal

Build an open-source parental-control system for YouTube on a shared Windows computer.

The system allows children to browse YouTube recommendations freely, but permits video playback **only from explicitly approved YouTube channels**.

A parent manages channel approvals remotely using a Telegram bot.

The system must work without cloud hosting and enforce **Default Deny**, which cannot be disabled through configuration.

## 2. Technology Stack

| Component | Technology |
|---|---|
| Browser extension | Chrome Manifest V3, TypeScript |
| Local backend | Go |
| Database | SQLite |
| Remote management | Telegram Bot API |
| Local communication | HTTP API on `127.0.0.1` |
| Deployment | Windows Service |
| Testing | Go tests, Vitest, Playwright |

Organize the project as a monorepo.

## 3. Core Requirements

### 3.1 YouTube Extension

- Run on YouTube pages, including navigation within its SPA.
- Allow browsing recommendations, search results and thumbnails.
- Detect the channel associated with a video before allowing playback.
- Identify channels using stable YouTube channel IDs, not display names.
- Block playback for every channel not explicitly approved.
- Display a Hebrew blocking overlay with the channel name and an approval-request button.
- Prevent duplicate requests for the same pending channel.
- React to configuration updates without requiring a browser restart.
- Fail closed when channel identification or backend communication fails.

Handle watch pages, Shorts, playlists, autoplay, embedded players where supported, and YouTube's dynamic navigation.

Do not rely exclusively on URL matching: video URLs generally do not contain the channel ID.

For the MVP, investigate reliable channel identification from YouTube page metadata and DOM. Avoid requiring a paid YouTube API.

**Critical:** Prevent playback while channel identity is unresolved. Add tests for navigation races, autoplay and temporary backend failures. Document any cases where pre-playback enforcement cannot be guaranteed.

### 3.2 Channel Authorization

Hardcode the following invariant:

`Unknown channel = Denied`

The configuration must never contain an `allowAll` or equivalent option.

Channel states:

- Approved — playback allowed.
- Unknown — blocked, approval can be requested.
- Explicitly blocked — blocked without a new approval request.

The backend is authoritative for permissions. The extension maintains a locally cached snapshot of approved channels for availability, but an absent or unverified permission always means deny.

### 3.3 Local Backend

Implement a Go service that:

- Runs on the children's Windows computer.
- Starts automatically using Windows Service management.
- Exposes an authenticated API bound only to loopback.
- Stores approved channels, pending requests, configuration versions and activity logs in SQLite.
- Integrates with Telegram using long polling.
- Supports clean shutdown and automatic recovery.
- Does not expose any inbound internet-facing port.

Suggested API:

| Endpoint | Purpose |
|---|---|
| `GET /health` | Service health |
| `GET /config` | Current approved channels and version |
| `POST /events` | Activity reporting |
| `POST /requests` | Request channel approval |
| `POST /heartbeat` | Extension activity signal |

Implement version-based configuration synchronization. Use polling initially; consider long polling later if needed.

### 3.4 Telegram Bot

The Telegram bot is the only parent-facing management interface for the MVP.

Commands:

- `/start` — introduction
- `/status` — service and extension status
- `/pending` — pending approval requests
- `/allow` — approve a channel
- `/block` — block or revoke approval
- `/channels` — list approved channels
- `/logs` — recent activity
- `/help` — command reference

Register commands with Telegram's `setMyCommands`.

When a child explicitly requests approval, send a Telegram message containing:

- Channel name
- Stable channel ID
- Video URL
- Request timestamp
- Inline Approve and Reject buttons

Only explicit approval requests generate immediate Telegram notifications.

Ordinary blocked playback attempts are logged locally without notifications.

Use the parent's configured Telegram user ID for authorization. Validate every command and callback, including callback data associated with old or already-resolved requests.

Keep the Telegram bot token exclusively in the backend.

### 3.5 Activity Logs

Record:

- Timestamp
- Event type
- Channel ID, when known
- Channel name, when known
- Video ID, when known
- Permission decision
- Approval request status
- Configuration version

Support querying recent events through Telegram.

Implement configurable retention with a sensible default of 30 days.

Do not record passwords, cookies, page contents or unrelated browsing activity.

### 3.6 Heartbeat and Monitoring

The extension reports:

- Extension version
- Configuration version
- Timestamp
- Active YouTube tab count, when available

The backend tracks the last successful extension heartbeat.

The `/status` command shows whether the extension was recently observed.

Chrome Manifest V3 service workers can be suspended. Do not assume a continuously running extension background process.

Distinguish between:

- Extension recently active
- Extension not recently observed
- Backend unavailable
- Browser or computer possibly inactive

Never report that the extension was deliberately disabled based only on a missing heartbeat.

### 3.7 Security

- Restrict Telegram management to authorized parent identities.
- Bind the local API to loopback.
- Authenticate extension-to-backend requests and validate message origins.
- Protect the SQLite database and service configuration with Windows filesystem permissions.
- Store the Telegram token securely, outside the extension.
- Use a standard Windows account for the children and a separate administrator account.
- Do not expose an endpoint that allows children to approve their own requests.
- Prevent configuration changes from enabling unrestricted playback.
- Document the limitations of extension-only enforcement, including other browsers, extension removal, Incognito mode and direct access to local service files.

This is parental-control software, not a tamper-proof security boundary.

## 4. Repository Structure

```text
youtube-guardian/
├── backend/
│   ├── cmd/
│   ├── internal/
│   │   ├── api/
│   │   ├── auth/
│   │   ├── channels/
│   │   ├── database/
│   │   ├── events/
│   │   ├── telegram/
│   │   └── service/
│   └── migrations/
├── extension/
│   ├── src/
│   │   ├── background/
│   │   ├── content/
│   │   ├── youtube/
│   │   ├── api/
│   │   └── ui/
│   └── manifest.json
├── tests/
├── scripts/
├── docs/
└── README.md
```

## 5. Implementation Phases

### Phase 1 — Foundation

Create the monorepo, Go backend, SQLite schema, migrations, configuration management and health endpoint.

### Phase 2 — YouTube Enforcement

Implement channel identification, permission checks, playback blocking, Hebrew overlay and navigation handling.

Start with ordinary watch pages, then extend to Shorts, playlists, autoplay and other relevant entry points.

### Phase 3 — Telegram Management

Implement bot setup, authorized commands, approval workflows, inline buttons and channel management.

### Phase 4 — Synchronization and Logs

Implement versioned configuration, local caching, activity events, retention and Telegram log queries.

### Phase 5 — Monitoring and Reliability

Implement heartbeat reporting, status classification, reconnection behavior and fail-closed tests.

### Phase 6 — Windows Deployment

Provide installation scripts, Windows Service registration, automatic startup, database location, configuration instructions and uninstall scripts.

### Phase 7 — End-to-End Testing

Automate browser tests where feasible and document manual tests for real YouTube behavior.

## 6. Acceptance Criteria

The project is complete when:

- A video from an approved channel plays normally.
- A video from an unknown channel is blocked before playback.
- An unknown channel can trigger an explicit approval request.
- The parent receives a Telegram message with working approval buttons.
- Approval propagates to Chrome without restarting it.
- Revoking approval blocks subsequent playback.
- Blocked attempts appear in logs but do not trigger unsolicited notifications.
- The system remains Default Deny when the backend is unavailable.
- The Telegram bot works remotely without router port forwarding.
- The backend starts automatically with Windows.
- Configuration and logs survive restart.
- Automated tests cover authorization, synchronization, Telegram callbacks and critical playback-blocking scenarios.

## 7. Instructions for Codex

Implement the project in incremental, testable phases.

Before coding, inspect the repository and create a detailed task breakdown.

Prefer simple, maintainable solutions over unnecessary infrastructure.

Do not introduce a cloud backend, paid service, web dashboard or unrelated website filtering.

Do not silently weaken Default Deny when encountering implementation difficulties.

Where YouTube or Chrome limitations prevent guaranteed enforcement, document the limitation and provide the safest practical behavior.

Run tests after each phase, fix failures, and update documentation.

Produce a working MVP, installation instructions and a final report describing implemented features, tests and known limitations.

## Phase 8 — YouTube Shorts Blocking (Future Feature)

**Goal:** Completely hide YouTube Shorts from the browsing experience, regardless of whether the channel is approved.

### Requirements

- Hide the Shorts section from the YouTube homepage.
- Hide Shorts from search results, recommendations and channel pages.
- Remove the Shorts entry from the navigation menu.
- Block direct access to `/shorts/*` URLs.
- Prevent Shorts from appearing during dynamic navigation.
- Apply filtering immediately, minimizing visible flashes of Shorts content.

### Implementation

- Use the existing Chrome extension's content scripts.
- Detect Shorts elements using DOM selectors and navigation events.
- Use CSS filtering where possible and MutationObserver where necessary.
- Redirect direct Shorts navigation to a neutral blocked page.
- Keep the feature independent of channel authorization.

### Acceptance Criteria

1. No Shorts section appears on the homepage.
2. Shorts do not appear in search results or recommendations.
3. Direct Shorts links cannot be played.
4. Shorts remain hidden after navigation without refreshing the page.
5. Regular YouTube videos continue working normally.

**Priority:** Future enhancement, after the core MVP is stable.

יש כאן הבחנה חשובה: גם אם אישרת ערוץ מסוים, **הסרטונים הרגילים שלו יהיו זמינים, אבל ה־Shorts שלו עדיין ייחסמו**.

אני ממליצה שהפיצ'ר הזה יהיה מופעל כברירת מחדל, אבל שתוכל לשנות אותו דרך פקודת Telegram כמו `/shorts_on` או `/shorts_off`, בניגוד לכלל החסימה של ערוצים לא מאושרים, שתמיד יישאר קבוע.

הייתי נותנת לקודקס את האפיון הזה, אבל מבקשת ממנו להתחיל בבדיקת היתכנות של חסימת ניגון לפני תחילת הסרטון. זו הנקודה הטכנית הכי רגישה בפרויקט, ועדיף לא לגלות בסוף שהחסימה אינה אמינה.

כשהתוכנית תעבור לקודקס, הוא יוכל לממש אותה בשלבים ולדווח אילו בדיקות עברו ואילו מגבלות נשארו.
