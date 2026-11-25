# Pre-playback feasibility probe

This experiment uses Playwright's Chrome for Testing and Manifest V3 against a local page with a muted, autoplaying WebM video. It records time progression and frames submitted to the compositor through `requestVideoFrameCallback`. It uses a fresh browser profile for each test.

Run from this directory with Node.js 20 or newer.

```sh
npm ci
npx playwright install chromium
npm test
```

With Chrome for Testing 159 on Linux, the baseline autoplayed. A content script at `document_start` caught the `play` event and paused the video. One run still reported one frame and roughly 0.7 ms of playback before the pause. The exact timing can vary. A frame reported by the compositor can include the initial frame decoded during preload.

A static `declarativeNetRequest` rule blocked the matching request both for direct playback and for `fetch`. The fixture server received neither request, and the player stayed at zero elapsed time with zero presented frames. The same page could play a blob created from media loaded at a different URL. A URL rule can only control requests that match it. It cannot remove data already available to the player.

The development environment returned 403 when attempting to reach YouTube. This probe covers the local fixture. For phase 2, check whether a deny-by-default network rule can cover YouTube's media paths and bind any exception to the current authorized video. Verify that exceptions cannot carry over across watch-page navigation, autoplay, playlists, Shorts, and embeds. Measure frame presentation when a player already holds media data or its backend authorization is unresolved. A pause handler on its own cannot guarantee that the first frame was withheld.

The sample is a synthetic test pattern generated with FFmpeg.

```sh
ffmpeg -f lavfi -i 'testsrc2=size=64x48:rate=15' -t 3 -an -c:v libvpx -b:v 30k sample.webm
```
