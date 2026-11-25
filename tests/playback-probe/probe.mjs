import assert from 'node:assert/strict'
import { once } from 'node:events'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { chromium } from 'playwright'

const fixture = await readFile(new URL('./sample.webm', import.meta.url))
const pageHTML = `<!doctype html>
<meta charset="utf-8">
<title>Guardian playback probe</title>
<video muted autoplay playsinline></video>
<script>
  const video = document.querySelector('video');
  let presentedFrames = 0;
  const onFrame = () => {
    presentedFrames++;
    video.requestVideoFrameCallback(onFrame);
  };
  video.requestVideoFrameCallback(onFrame);
  window.observe = () => ({
    currentTime: video.currentTime,
    paused: video.paused,
    presentedFrames,
  });
  if (location.pathname === '/blob') {
    fetch('/separate-source.webm')
      .then(response => response.blob())
      .then(blob => { video.src = URL.createObjectURL(blob); });
  } else {
    video.src = '/media.webm';
  }
</script>`

let mediaRequests = 0
const server = createServer((req, res) => {
  if (req.url === '/media.webm' || req.url === '/separate-source.webm') {
    if (req.url === '/media.webm') mediaRequests++
    res.writeHead(200, {
      'Content-Type': 'video/webm',
      'Content-Length': fixture.length,
      'Cache-Control': 'no-store',
    })
    res.end(fixture)
    return
  }
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
  res.end(pageHTML)
})

server.listen(0, '127.0.0.1')
await once(server, 'listening')
const origin = `http://127.0.0.1:${server.address().port}`
const temp = await mkdtemp(join(tmpdir(), 'guardian-playback-'))

try {
  await probe('baseline', {}, async page => {
    await page.goto(origin)
    await expectPlayback(page)
    assert.ok(mediaRequests > 0)
    console.log('Baseline autoplay', await page.evaluate(() => window.observe()))
  })

  await probe('pause', {
    content_scripts: [{
      matches: ['http://127.0.0.1/*'],
      js: ['pause.js'],
      run_at: 'document_start',
    }],
  }, async page => {
    await page.goto(origin)
    await page.waitForFunction(() => document.querySelector('video').dataset.guardianPaused)
    const result = await page.evaluate(() => window.observe())
    assert.equal(result.paused, true)
    console.log('Document-start capture of play event', result)
  })

  await probe('network', {
    permissions: ['declarativeNetRequest'],
    host_permissions: ['http://127.0.0.1/*'],
    declarative_net_request: {
      rule_resources: [{ id: 'deny_media', enabled: true, path: 'rules.json' }],
    },
  }, async page => {
    const initialRequests = mediaRequests
    const [failedRequest] = await Promise.all([
      page.waitForEvent('requestfailed', request =>
        request.url() === `${origin}/media.webm` && request.resourceType() === 'media'),
      page.goto(origin),
    ])
    assert.match(failedRequest.failure().errorText, /ERR_BLOCKED_BY_CLIENT/)
    await page.waitForFunction(() => document.querySelector('video').error !== null)
    const result = await page.evaluate(() => window.observe())
    assert.deepEqual(result, { currentTime: 0, paused: true, presentedFrames: 0 })

    const fetched = await page.evaluate(async () => {
      try {
        await fetch('/media.webm')
        return true
      } catch {
        return false
      }
    })
    assert.equal(fetched, false)
    assert.equal(mediaRequests, initialRequests)
    console.log('Network gate blocked video and fetch', result)

    await page.goto(`${origin}/blob`)
    await expectPlayback(page)
    assert.equal(mediaRequests, initialRequests)
    console.log('Blob sourced from a different URL played', await page.evaluate(() => window.observe()))
  })
} finally {
  server.close()
  await rm(temp, { recursive: true, force: true })
}

async function expectPlayback(page) {
  await page.waitForFunction(() => {
    const { currentTime, presentedFrames } = window.observe()
    return currentTime > 0.2 && presentedFrames > 1
  })
}

async function probe(name, extension, run) {
  const args = ['--autoplay-policy=no-user-gesture-required']
  if (name !== 'baseline') {
    const extensionPath = join(temp, name)
    await mkdir(extensionPath)
    await writeFile(join(extensionPath, 'manifest.json'), JSON.stringify({
      name: `Guardian feasibility probe (${name})`,
      version: '1.0.0',
      manifest_version: 3,
      ...extension,
    }))
    if (name === 'pause') {
      await writeFile(join(extensionPath, 'pause.js'), `
        document.addEventListener('play', event => {
          if (!(event.target instanceof HTMLVideoElement)) return;
          event.target.pause();
          event.target.dataset.guardianPaused = 'true';
        }, true);
      `)
    }
    if (name === 'network') {
      await writeFile(join(extensionPath, 'rules.json'), JSON.stringify([{
        id: 1,
        action: { type: 'block' },
        condition: {
          regexFilter: '^http://127[.]0[.]0[.]1:[0-9]+/media[.]webm$',
          resourceTypes: ['media', 'xmlhttprequest'],
        },
      }]))
    }
    args.push(`--disable-extensions-except=${extensionPath}`, `--load-extension=${extensionPath}`)
  }

  const context = await chromium.launchPersistentContext(join(temp, `${name}-profile`), {
    channel: 'chromium',
    executablePath: process.env.CHROMIUM_PATH,
    args,
  })
  try {
    const page = context.pages()[0]
    page.setDefaultTimeout(10_000)
    await run(page)
  } finally {
    await context.close()
  }
}
