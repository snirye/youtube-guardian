import { pollMs, sameTarget, targetOf, validIdentity } from './model'
import type { Check, Decision, Identity, Target } from './model'

let identity: Identity | undefined
let target = targetOf(location.href)
let generation = 0
let inFlight = false
let queued = false
let notBefore = 0
let lastEvent = ''
let grantUntil = 0
let grantTimer: ReturnType<typeof setTimeout> | undefined
const requestedChannels = new Set<string>()
const submittingChannels = new Set<string>()
let unresolvedSince = Date.now()
let notice: HTMLElement | undefined
let label: HTMLElement | undefined
let action: HTMLButtonElement | undefined
let state: Decision = 'unresolved'

const text: Record<Exclude<Decision, 'approved'>, string> = {
  unresolved: 'ממתינים לזיהוי הערוץ לפני תחילת הניגון.',
  unknown: 'הערוץ עדיין לא אושר לצפייה.',
  blocked: 'הערוץ חסום לצפייה.',
  unavailable: 'לא ניתן לאמת הרשאת צפייה כרגע.',
  unsupported: 'לא ניתן לאמת את הניגון בנגן הזה.',
  shorts: 'הצפייה בסרטוני Shorts חסומה.',
}

function makeOverlay() {
  if (!document.body || notice?.isConnected) return
  notice = document.createElement('aside')
  notice.id = 'guardian-overlay'
  notice.lang = 'he'
  notice.dir = 'rtl'
  const title = document.createElement('strong')
  title.textContent = 'הניגון חסום'
  label = document.createElement('p')
  label.setAttribute('role', 'status')
  action = document.createElement('button')
  action.type = 'button'
  action.textContent = 'בקשו אישור מההורים'
  action.addEventListener('click', async event => {
    if (!event.isTrusted || !identity || state !== 'unknown' || !action || action.disabled) return
    const requested = identity
    const current = generation
    submittingChannels.add(requested.channelId)
    render(target, state, requested)
    const ok = await chrome.runtime.sendMessage({ type: 'request', identity: requested }).catch(() => false)
    submittingChannels.delete(requested.channelId)
    if (ok) requestedChannels.add(requested.channelId)
    if (current !== generation) return
    render(target, state, identity)
    if (!ok && state === 'unknown') action.textContent = 'השליחה נכשלה. נסו שוב'
  })
  notice.append(title, label, action)
  document.body.append(notice)
}

function render(active: Target | undefined, decision: Decision, details?: Identity) {
  if (!active || decision === 'approved') {
    notice?.remove()
    return
  }
  makeOverlay()
  if (!label || !action) return
  notice?.setAttribute('data-decision', decision)
  label.textContent = details?.channelName ? `${details.channelName} — ${text[decision]}` : text[decision]
  action.hidden = decision !== 'unknown' || !details
  const waiting = !!details && requestedChannels.has(details.channelId)
  const sending = !!details && submittingChannels.has(details.channelId)
  action.disabled = waiting || sending
  action.textContent = waiting ? 'בקשת האישור ממתינה' : sending ? 'שולחים בקשה…' : 'בקשו אישור מההורים'
}

function revoke() {
  clearTimeout(grantTimer)
  grantUntil = 0
  document.dispatchEvent(new Event('guardian:revoke'))
}

function navigation(early = false) {
  generation++
  target = targetOf(location.href)
  notBefore = early ? Date.now() + 150 : 0
  identity = undefined
  state = 'unresolved'
  lastEvent = ''
  unresolvedSince = Date.now()
  revoke()
  render(target, state)
  if (window === window.top) void chrome.runtime.sendMessage({ type: 'revoke' }).catch(() => undefined)
  setTimeout(() => void refresh(), early ? 160 : 0)
}
document.addEventListener('guardian:navigation', () => navigation(true))
document.addEventListener('yt-navigate-finish', () => navigation())
function inspect(): Identity | undefined {
  if (!target) return
  const result: { identity?: Identity } = {}
  const handler = (event: Event) => {
    const candidate = (event as CustomEvent).detail
    if (validIdentity(candidate) && candidate.videoId === target?.id) result.identity = candidate
  }
  document.addEventListener('guardian:identity', handler)
  document.dispatchEvent(new Event('guardian:inspect'))
  document.removeEventListener('guardian:identity', handler)
  return result.identity
}

function logPlayback(check: Check) {
  if (!target || check.version === null || window !== window.top) return
  if (check.decision === 'unresolved' && Date.now() - unresolvedSince < 3500) return
  const decision = check.decision === 'approved' ? 'approved' : 'denied'
  const key = `${target.id}/${identity?.channelId ?? ''}/${decision}`
  if (lastEvent === key) return
  lastEvent = key
  void chrome.runtime.sendMessage({
    type: 'event', videoId: target.id, channelId: identity?.channelId ?? '',
    channelName: identity?.channelName ?? '', decision, version: check.version,
  }).catch(() => undefined)
}

async function refresh() {
  if (!sameTarget(target, targetOf(location.href))) {
    navigation()
    return
  }
  if (Date.now() < notBefore) return
  if (inFlight) {
    queued = true
    return
  }
  inFlight = true
  try {
    const current = generation
    const previousIdentity = identity
    identity = inspect()
    if (previousIdentity && (previousIdentity.channelId !== identity?.channelId
        || previousIdentity.assets.join() !== identity.assets.join())) revoke()
    const check: Check = await chrome.runtime.sendMessage({ type: 'check', identity }).catch(() => undefined)
      ?? { decision: 'unavailable', version: null, blockShorts: true }
    if (generation !== current || !sameTarget(target, targetOf(location.href))) return
    document.documentElement?.toggleAttribute('data-guardian-show-shorts', !check.blockShorts)
    if (check.blockShorts && target?.kind === 'shorts') {
      revoke()
      logPlayback(check)
      if (window === window.top) location.replace(chrome.runtime.getURL('shorts.html'))
      else render(target, 'shorts')
      return
    }
    state = check.decision
    if (check.decision === 'approved' && identity && check.expires && Date.now() < check.expires) {
      grantUntil = check.expires
      document.dispatchEvent(new CustomEvent('guardian:grant', { detail: { ...identity, expires: grantUntil } }))
      if (!document.documentElement?.hasAttribute('data-guardian-allowed')) {
        revoke()
        state = 'unresolved'
      } else {
        clearTimeout(grantTimer)
        grantTimer = setTimeout(() => {
          state = 'unavailable'
          revoke()
          render(target, state, identity)
        }, Math.max(0, grantUntil - Date.now()))
      }
    } else {
      revoke()
    }
    if (identity && (state === 'approved' || state === 'blocked')) requestedChannels.delete(identity.channelId)
    if (identity && state === 'unknown' && check.pending) requestedChannels.add(identity.channelId)
    render(target, state, identity)
    logPlayback({ ...check, decision: state })
  } finally {
    inFlight = false
    if (queued) {
      queued = false
      void refresh()
    }
  }
}

function start() {
  render(target, state)
  void refresh()
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, { once: true })
else start()
setInterval(() => void refresh(), pollMs)
