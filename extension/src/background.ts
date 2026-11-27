import {
  assetPattern, channelPattern, leaseMs, sameTarget, supportedHost, targetOf, tokenPattern, validIdentity, videoPattern,
} from './model'
import type { Check, Identity, Target } from './model'

type Snapshot = {
  version: number
  blockShorts: boolean
  pending: string[]
  channels: { id: string; decision: 'approved' | 'blocked' }[]
}

let snapshot: Snapshot | undefined
let reading: Promise<Snapshot> | undefined
const changes = new Map<number, Promise<unknown>>()

async function request(path: string, body?: unknown): Promise<Response> {
  const keys = ['apiToken', 'port']
  const [managed, local] = await Promise.all([
    chrome.storage.managed.get(keys).catch((): Record<string, unknown> => ({})),
    chrome.storage.local.get(keys),
  ])
  const token = managed.apiToken ?? local.apiToken
  const port = managed.port ?? local.port ?? 8765
  if (typeof token !== 'string' || !tokenPattern.test(token) || typeof port !== 'number' || !Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error('local API is not configured')
  }
  return fetch(`http://127.0.0.1:${port}${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: 'no-store',
    credentials: 'omit',
    redirect: 'error',
    signal: AbortSignal.timeout(1400),
  })
}

function getSnapshot(): Promise<Snapshot> {
  if (!reading) {
    reading = (async () => {
      const response = await request('/config')
      if (!response.ok) throw new Error('local API unavailable')
      const data: Snapshot = await response.json()
      if (!Number.isSafeInteger(data.version) || data.version < 1 || typeof data.blockShorts !== 'boolean'
          || !Array.isArray(data.pending) || data.pending.some(id => !channelPattern.test(id))
          || !Array.isArray(data.channels)
          || data.channels.some(c => !channelPattern.test(c?.id) || !['approved', 'blocked'].includes(c?.decision))) {
        throw new Error('invalid permission snapshot')
      }
      snapshot = data
      return data
    })().catch(error => {
      snapshot = undefined
      throw error
    }).finally(() => { reading = undefined })
  }
  return reading
}

function queue<T>(tabId: number, task: () => Promise<T>): Promise<T> {
  const pending = (changes.get(tabId) ?? Promise.resolve()).catch(() => undefined).then(task)
  changes.set(tabId, pending)
  const cleanup = () => { if (changes.get(tabId) === pending) changes.delete(tabId) }
  void pending.then(cleanup, cleanup)
  return pending
}

async function updateRule(tabId: number, assets: string[] = []): Promise<void> {
  const id = tabId + 1
  const [previous] = await chrome.declarativeNetRequest.getSessionRules({ ruleIds: [id] })
  const assetIDs = [...new Set(assets.filter(asset => assetPattern.test(asset)))].sort()
  const regex = assetIDs.length ? '^https://[a-z0-9.-]+[.]googlevideo[.]com/videoplayback[?]([^#]*&)?id=('
    + assetIDs.map(asset => asset.replaceAll('.', '[.]')).join('|') + ')(&|$)' : undefined
  if (previous?.condition.regexFilter === regex) return

  const rule: chrome.declarativeNetRequest.Rule | undefined = regex ? {
    id,
    priority: 2,
    action: { type: 'allow' },
    condition: {
      initiatorDomains: ['youtube.com', 'youtube-nocookie.com'],
      tabIds: [tabId],
      regexFilter: regex,
      isUrlFilterCaseSensitive: true,
      resourceTypes: ['media', 'xmlhttprequest'],
    },
  } : undefined
  await chrome.declarativeNetRequest.updateSessionRules({
    removeRuleIds: previous ? [id] : [],
    addRules: rule ? [rule] : [],
  })
}

const unavailable: Check = { decision: 'unavailable', version: null, blockShorts: true }

async function currentFrame(tabId: number, frameId: number, documentId: string | undefined, target?: Target) {
  const frame = await chrome.webNavigation.getFrame({ tabId, frameId }).catch(() => null)
  if (!frame || frame.documentId !== documentId) return false
  // In an SPA, the top-level tab URL can advance while the original document remains.
  const url = frameId === 0 ? (await chrome.tabs.get(tabId).catch(() => null))?.url : frame.url
  return !!url && sameTarget(targetOf(url), target)
}

async function check(tabId: number, frameId: number, documentId: string | undefined, target: Target | undefined,
  identity?: Identity): Promise<Check> {
  const data = await getSnapshot().catch(() => undefined)
  if (frameId !== 0) return { ...(data ? { version: data.version, blockShorts: data.blockShorts } : unavailable), decision: 'unsupported' }
  return queue(tabId, async () => {
    if (!await currentFrame(tabId, frameId, documentId, target)) return unavailable
    let decision: Check['decision'] = 'unresolved'
    if (!data) decision = 'unavailable'
    else if (data.blockShorts && target?.kind === 'shorts') decision = 'shorts'
    else if (identity) {
      decision = data.channels.find(channel => channel.id === identity.channelId)?.decision ?? 'unknown'
      if (decision === 'approved' && identity.assets.length === 0) decision = 'unsupported'
    }
    try {
      await updateRule(tabId, decision === 'approved' ? identity?.assets : [])
    } catch {
      await updateRule(tabId).catch(() => undefined)
      decision = 'unsupported'
    }
    const expires = decision === 'approved' ? Date.now() + leaseMs : undefined
    return {
      decision, version: data?.version ?? null, blockShorts: data?.blockShorts ?? true, expires,
      pending: !!identity && (data?.pending.includes(identity.channelId) ?? false),
    }
  })
}

function clear(tabId: number) {
  void queue(tabId, () => updateRule(tabId)).catch(() => undefined)
}

chrome.tabs.onRemoved.addListener(clear)
chrome.webNavigation.onBeforeNavigate.addListener(({ tabId, frameId }) => {
  if (frameId === 0) clear(tabId)
})
chrome.webNavigation.onHistoryStateUpdated.addListener(({ tabId, frameId }) => {
  if (frameId === 0) clear(tabId)
})

chrome.runtime.onMessage.addListener((msg, sender, respond) => {
  if (sender.id !== chrome.runtime.id || sender.tab?.id === undefined || sender.frameId === undefined || !sender.url
      || !msg || typeof msg !== 'object') return
  const tabId = sender.tab.id
  const url = new URL(sender.url)
  if (url.protocol !== 'https:' || !supportedHost(url.hostname)) return
  if (msg.type === 'revoke') {
    if (sender.frameId === 0) clear(tabId)
    return
  }
  if (typeof msg.href !== 'string') return
  let href: URL
  try {
    href = new URL(msg.href)
  } catch {
    return
  }
  if (href.origin !== url.origin) return
  const target = targetOf(href.href)
  if (msg.type === 'check') {
    const identity = validIdentity(msg.identity) && msg.identity.videoId === target?.id ? msg.identity : undefined
    void check(tabId, sender.frameId, sender.documentId, target, identity)
      .then(respond, () => respond(unavailable))
    return true
  }
  if (sender.frameId !== 0 || !target) return
  const isCurrent = () => currentFrame(tabId, 0, sender.documentId, target)
  if (msg.type === 'request' && validIdentity(msg.identity) && msg.identity.videoId === target.id) {
    const { channelId, channelName, videoId } = msg.identity
    void isCurrent().then(async current => current && (await request('/requests', { channelId, channelName, videoId })).ok)
      .then(respond, () => respond(false))
    return true
  }
  if (msg.type === 'event' && msg.videoId === target.id && videoPattern.test(msg.videoId)) {
    const { channelId, channelName, videoId, decision, version } = msg
    void isCurrent().then(current => {
      if (current) return request('/events', { channelId, channelName, videoId, decision, version })
    }).catch(() => undefined)
  }
})

async function heartbeat() {
  const [data, tabs] = await Promise.all([
    getSnapshot().catch(() => undefined),
    chrome.tabs.query({ url: ['https://www.youtube.com/*', 'https://m.youtube.com/*'] }).catch(() => undefined),
  ])
  await request('/heartbeat', {
    extensionVersion: chrome.runtime.getManifest().version,
    configurationVersion: data?.version ?? snapshot?.version ?? null,
    activeTabs: tabs?.length ?? null,
  })
}
function startHeartbeat() {
  void chrome.alarms.get('heartbeat').then(alarm => {
    if (!alarm) return chrome.alarms.create('heartbeat', { periodInMinutes: 0.5 })
  }).catch(() => undefined)
  void heartbeat().catch(() => undefined)
}
chrome.alarms.onAlarm.addListener(alarm => {
  if (alarm.name === 'heartbeat') void heartbeat().catch(() => undefined)
})
chrome.runtime.onInstalled.addListener(startHeartbeat)
chrome.runtime.onStartup.addListener(startHeartbeat)
chrome.storage.onChanged.addListener((_, area) => {
  if (area === 'managed' || area === 'local') startHeartbeat()
})
