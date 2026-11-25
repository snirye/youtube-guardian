import assert from 'node:assert/strict'
import { execFileSync, spawn } from 'node:child_process'
import { createHash, randomBytes, X509Certificate } from 'node:crypto'
import { once } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer as createHTTP } from 'node:http'
import { createServer as createHTTPS } from 'node:https'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const root = fileURLToPath(new URL('../../', import.meta.url))
const fixture = await readFile(new URL('./fixture.html', import.meta.url))
const media = await readFile(join(root, 'tests/playback-probe/sample.webm'))
const temp = await mkdtemp(join(tmpdir(), 'guardian-e2e-'))
const demo = process.env.GUARDIAN_RECORD_DIR ? resolve(process.env.GUARDIAN_RECORD_DIR) : undefined
const token = randomBytes(32).toString('hex')
const firstChannel = 'UC' + 'a'.repeat(22)
const secondChannel = 'UC' + 'b'.repeat(22)
const parent = { id: 42 }
const chat = { id: 42, type: 'private' }
let nextUpdate = 1
const updates = []
const calls = []
const traffic = []
let context, child, telegram, fixtureServer
const extensionID = 'jcpbnaipmnmgfoelllffgphnfbbngdik'

try {
  const key = join(temp, 'key.pem')
  const cert = join(temp, 'cert.pem')
  execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
    '-subj', '/CN=www.youtube.com', '-addext', 'subjectAltName=DNS:www.youtube.com,DNS:r1.googlevideo.com',
    '-keyout', key, '-out', cert], { stdio: 'ignore' })
  const certBytes = await readFile(cert)
  const spki = new X509Certificate(certBytes).publicKey.export({ type: 'spki', format: 'der' })
  const fingerprint = createHash('sha256').update(spki).digest('base64')
  fixtureServer = createHTTPS({ key: await readFile(key), cert: certBytes }, (req, res) => {
    const url = new URL(req.url, 'https://' + req.headers.host)
    if (url.hostname === 'r1.googlevideo.com' && url.pathname === '/videoplayback') {
      traffic.push(url.searchParams.get('id'))
      res.writeHead(200, { 'Content-Type': 'video/webm', 'Content-Length': media.length, 'Cache-Control': 'no-store' })
      res.end(media)
    } else if (url.hostname === 'www.youtube.com') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
      res.end(fixture)
    } else {
      res.writeHead(404).end()
    }
  })
  const fixturePort = await listen(fixtureServer)
  telegram = createHTTP(async (req, res) => {
    const method = req.url.split('/').at(-1)
    const chunks = []
    for await (const chunk of req) chunks.push(chunk)
    const data = JSON.parse(Buffer.concat(chunks))
    calls.push({ method, data })
    let result = true
    if (method === 'getUpdates') {
      result = updates.filter(update => update.update_id >= data.offset)
      if (!result.length) await new Promise(resolve => setTimeout(resolve, 120))
    }
    res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ ok: true, result }))
  })
  const telegramPort = await listen(telegram)
  const port = await freePort()
  const backend = join(temp, process.platform === 'win32' ? 'guardian.test.exe' : 'guardian.test')
  execFileSync('go', ['test', '-c', '-o', backend, './cmd/guardian'], { cwd: join(root, 'backend'), stdio: 'inherit' })
  const configPath = join(temp, 'guardian.json')
  await writeFile(configPath, JSON.stringify({
    api_token: token, port, parent_telegram_user_id: parent.id, telegram_bot_token: '123456:local-fixture-token-only',
  }))
  const local = `http://127.0.0.1:${port}`
  function startBackend() {
    child = spawn(backend, ['-test.run=^TestBrowserBackend$'], {
      env: { ...process.env, GUARDIAN_TEST_CONFIG: configPath, GUARDIAN_TEST_TELEGRAM: `http://127.0.0.1:${telegramPort}` },
      stdio: ['ignore', 'ignore', 'inherit'],
    })
    return eventually(async () => (await fetch(local + '/health', { headers: { Authorization: 'Bearer ' + token } })).ok)
  }
  await startBackend()
  const snapshot = () => fetch(local + '/config', { headers: { Authorization: 'Bearer ' + token } }).then(r => r.json())

  const ext = join(root, 'extension/dist')
  context = await chromium.launchPersistentContext(join(temp, 'profile'), {
    channel: 'chromium',
    viewport: { width: 1080, height: 760 },
    ...(demo ? { recordVideo: { dir: demo, size: { width: 1080, height: 760 } } } : {}),
    args: [
      `--disable-extensions-except=${ext}`, `--load-extension=${ext}`,
      '--autoplay-policy=no-user-gesture-required', '--no-proxy-server',
      `--ignore-certificate-errors-spki-list=${fingerprint}`,
      `--host-resolver-rules=MAP www.youtube.com 127.0.0.1:${fixturePort}, MAP r1.googlevideo.com 127.0.0.1:${fixturePort}, MAP * ~NOTFOUND, EXCLUDE 127.0.0.1`,
    ],
  })
  const page = context.pages()[0]
  page.setDefaultTimeout(12000)
  if (process.env.GUARDIAN_DEBUG) {
    page.on('console', message => console.log('Browser', message.text()))
    page.on('requestfailed', request => console.log('Request failed', request.url(), request.failure()))
    page.on('request', request => { if (request.url().includes('googlevideo')) console.log('Request', request.url()) })
  }
  const setup = await context.newPage()
  await setup.goto(`chrome-extension://${extensionID}/options.html`)
  await setup.locator('#token').fill(token)
  await setup.locator('#port').fill(String(port))
  await setup.locator('button').click()
  await setup.getByText('ההגדרות נשמרו.').waitFor()
  await setup.close()

  const overlay = page.locator('#guardian-overlay')
  const origin = 'https://www.youtube.com'
  await page.goto(origin + '/watch?v=approvedabc')
  await overlay.locator('[role=status]').getByText(/לא אושר/).waitFor()
  await page.evaluate(() => window.describe('1. ערוץ ללא אישור — אפס פריימים'))
  assert.deepEqual(await page.evaluate(() => window.observe()), { time: 0, frames: 0, paused: true, muted: true, requestCount: 0 })
  assert.deepEqual(traffic, [])
  assert.equal(calls.some(call => call.method === 'sendMessage'), false)
  assert.equal(await page.locator('ytd-reel-shelf-renderer').isVisible(), false)
  await beat(demo)

  await overlay.getByRole('button').click()
  await overlay.getByText('בקשת האישור ממתינה').waitFor()
  await page.evaluate(() => window.describe('2. נשלחה בקשת אישור'))
  const sent = await eventually(() => calls.find(call => call.method === 'sendMessage' && call.data.reply_markup))
  assert.match(sent.data.text, new RegExp(firstChannel))
  const initialVersion = (await snapshot()).version
  await page.reload()
  await overlay.getByText('בקשת האישור ממתינה').waitFor()
  await page.evaluate(() => window.describe('2. בקשת האישור נשמרה וממתינה להורה'))
  assert.equal(await overlay.getByRole('button').isDisabled(), true)
  assert.equal(calls.filter(call => call.method === 'sendMessage' && call.data.reply_markup).length, 1)
  const button = sent.data.reply_markup.inline_keyboard[0][0].callback_data
  emit({ callback_query: { id: 'foreign', from: { id: 43 }, message: { chat, message_id: 1 }, data: button } })
  await eventually(() => calls.some(call => call.method === 'getUpdates' && call.data.offset > 1))
  assert.deepEqual((await snapshot()).channels, [])
  await beat(demo)

  emit({ callback_query: { id: 'approved', from: parent, message: { chat, message_id: 1 }, data: button } })
  await overlay.waitFor({ state: 'detached' })
  await page.evaluate(() => window.describe('3. ההורה אישר — הסרטון מתנגן'))
  await page.waitForFunction(() => window.observe().time > 0.2 && window.observe().frames > 1)
  assert.ok(traffic.includes('asset-approved'))
  const approved = await snapshot()
  assert.equal(approved.channels[0].decision, 'approved')
  assert.ok(approved.version > initialVersion)
  if (demo) await page.waitForTimeout(2000)

  await page.getByRole('link', { name: 'סרטון שני' }).click()
  await page.locator('#guardian-overlay[data-decision=unknown]').getByText(/ערוץ נוסף/).waitFor()
  await page.evaluate(() => window.describe('4. מעבר לסרטון אחר — האישור הקודם אינו עובר'))
  assert.equal(await page.evaluate(() => window.observe().time), 0)
  assert.equal(await page.evaluate(() => window.observe().frames), 0)
  assert.equal(traffic.includes('asset-unknown'), false)
  assert.equal((await snapshot()).channels.some(channel => channel.id === secondChannel), false)
  await beat(demo)

  await page.getByRole('link', { name: 'סרטון ראשון' }).click()
  await overlay.waitFor({ state: 'detached' })
  await page.waitForFunction(() => window.observe().time > 0.2)
  await page.evaluate(() => window.describe('חזרה לסרטון המאושר'))
  emit({ message: { from: parent, chat, text: '/status' } })
  await eventually(() => calls.some(call => call.method === 'sendMessage' && /recently active/.test(call.data.text)))
  emit({ message: { from: parent, chat, text: `/block ${firstChannel}` } })
  await overlay.getByText(/הערוץ חסום/).waitFor()
  await page.evaluate(() => window.describe('5. האישור בוטל — הניגון נעצר'))
  assert.equal(await page.evaluate(() => window.observe().paused), true)
  assert.equal(await overlay.getByRole('button').count(), 0)
  emit({ callback_query: { id: 'stale', from: parent, message: { chat, message_id: 1 }, data: button } })
  await eventually(() => calls.some(call => call.method === 'answerCallbackQuery' && call.data.callback_query_id === 'stale'))
  assert.equal((await snapshot()).channels[0].decision, 'blocked')
  emit({ message: { from: parent, chat, text: '/logs' } })
  await eventually(() => calls.some(call => call.method === 'sendMessage' && /playback denied/.test(call.data.text)))
  assert.equal(calls.filter(call => call.method === 'sendMessage' && call.data.reply_markup).length, 1)
  await beat(demo)

  await page.evaluate(() => window.describe('חידוש האישור לפני בדיקת ניתוק השרת'))
  emit({ message: { from: parent, chat, text: `/allow ${firstChannel}` } })
  await overlay.waitFor({ state: 'detached' })
  await page.waitForFunction(() => !window.observe().paused)
  await stopBackend()
  await overlay.getByText(/לא ניתן לאמת/).waitFor()
  await page.evaluate(() => window.describe('6. השרת אינו זמין — החסימה חוזרת'))
  assert.equal(await page.evaluate(() => window.observe().paused), true)
  await beat(demo)

  await startBackend()
  await overlay.waitFor({ state: 'detached' })
  assert.equal((await snapshot()).channels[0].decision, 'approved')
  await page.goto(origin + '/shorts/approvedabc', { waitUntil: 'domcontentloaded' }).catch(error => {
    if (!error.message.includes('ERR_ABORTED')) throw error
  })
  await page.waitForURL(`chrome-extension://${extensionID}/shorts.html`)
  await page.getByRole('heading', { name: 'הצפייה ב־Shorts חסומה' }).waitFor()
  await beat(demo)
  emit({ message: { from: parent, chat, text: '/shorts_off' } })
  await eventually(async () => !(await snapshot()).blockShorts)
  await page.goto(origin + '/shorts/approvedabc')
  try {
    await page.waitForFunction(() => window.observe().time > 0.2, null, { timeout: 9000 })
  } catch (error) {
    console.error('Shorts state', page.url(), await overlay.textContent({ timeout: 500 }).catch(() => ''),
      await page.evaluate(() => {
        const v = document.querySelector('video')
        return { ...window.observe?.(), src: v?.src, currentSrc: v?.currentSrc, error: v?.error?.message,
          network: v?.networkState, ready: v?.readyState, autoplay: v?.autoplay,
          allowed: document.documentElement.hasAttribute('data-guardian-allowed') }
      }).catch(() => undefined), traffic)
    throw error
  }
  if (demo) {
    await page.evaluate(() => window.describe('7. ההורה שינה את הגדרת Shorts — כללי הערוצים עדיין חלים'))
    await beat(demo)
  }
  console.log('Chrome E2E passed. Tested blocking before first frame, Telegram request/approval/revocation, SPA navigation, heartbeat, backend outage/restart, and Shorts.')
  await context.close()
  context = undefined
  if (demo) {
    const recorded = await page.video().path()
    console.log('Recorded: ' + recorded)
    await writeFile(join(demo, 'recording-path.txt'), recorded)
  }
} finally {
  await context?.close()
  if (child && child.exitCode === null) await stopBackend()
  for (const server of [fixtureServer, telegram]) {
    server?.close()
    server?.closeAllConnections()
  }
  await rm(temp, { recursive: true, force: true })
}

function emit(data) {
  updates.push({ ...data, update_id: nextUpdate++ })
}

async function stopBackend() {
  if (!child || child.exitCode !== null) return
  const stopped = once(child, 'exit')
  child.kill('SIGTERM')
  const [code] = await stopped
  assert.equal(code, 0)
}

async function listen(server) {
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  return server.address().port
}
async function freePort() {
  const server = createHTTP()
  const port = await listen(server)
  await new Promise(resolve => server.close(resolve))
  return port
}
async function eventually(callback) {
  const deadline = Date.now() + 12000
  do {
    const result = await Promise.resolve().then(callback).catch(() => undefined)
    if (result) return result
    await new Promise(resolve => setTimeout(resolve, 100))
  } while (Date.now() < deadline)
  throw new Error('Timed out waiting for a test condition')
}
async function beat(recording) {
  if (recording) await new Promise(resolve => setTimeout(resolve, 2300))
}
