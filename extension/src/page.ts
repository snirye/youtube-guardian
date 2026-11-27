import { assetPattern, channelPattern, leaseMs, sameTarget, targetOf } from './model'
import type { Identity, Target } from './model'

type PlayerResponse = {
  videoDetails?: { videoId?: string; channelId?: string; author?: string }
  streamingData?: {
    formats?: Stream[]
    adaptiveFormats?: Stream[]
    serverAbrStreamingUrl?: string
    dashManifestUrl?: string
    hlsManifestUrl?: string
  }
}
type Stream = { url?: string; signatureCipher?: string; cipher?: string }
type YouTubePlayer = HTMLElement & {
  getVideoData?: () => { video_id?: string }
  getPlayerResponse?: () => PlayerResponse
}

const initialTarget = targetOf(location.href)
const originalPlay = HTMLMediaElement.prototype.play
const originalPause = HTMLMediaElement.prototype.pause
const restoreMute = new Map<HTMLMediaElement, boolean>()
const restoreAutoplay = new Set<HTMLMediaElement>()
const attempted = new Set<HTMLMediaElement>()
let grant: { target: Target; channelId: string; expires: number; assets: string[] } | undefined
let expiry: ReturnType<typeof setTimeout> | undefined

function streamAsset(raw?: string): string | undefined {
  if (!raw) return
  try {
    const url = new URL(raw)
    if (url.protocol !== 'https:' || !url.hostname.endsWith('.googlevideo.com') || url.pathname !== '/videoplayback') return
    const ids = url.searchParams.getAll('id')
    if (ids.length === 1 && assetPattern.test(ids[0])) return ids[0]
  } catch {
    return
  }
}

function inspect(): Identity | undefined {
  const target = targetOf(location.href)
  if (!target) return
  try {
    const player = document.querySelector<YouTubePlayer>('#movie_player, #shorts-player')
    const currentVideo = player?.getVideoData?.()?.video_id
    if (currentVideo && currentVideo !== target.id) return

    const response = player?.getPlayerResponse?.() ??
      (sameTarget(target, initialTarget) ? (window as Window & { ytInitialPlayerResponse?: PlayerResponse }).ytInitialPlayerResponse : undefined)
    const details = response?.videoDetails
    if (details?.videoId !== target.id || !details.channelId || !channelPattern.test(details.channelId)) return

    const streams = response?.streamingData
    const urls = [
      streams?.serverAbrStreamingUrl, streams?.dashManifestUrl, streams?.hlsManifestUrl,
      ...[...(streams?.formats ?? []), ...(streams?.adaptiveFormats ?? [])].map(stream =>
        stream.url ?? new URLSearchParams(stream.signatureCipher ?? stream.cipher).get('url') ?? undefined),
    ]
    const assets = [...new Set(urls.map(streamAsset).filter(asset => asset !== undefined))].sort()
    const name = typeof details.author === 'string' ? details.author.trim() : ''
    const safeName = new TextDecoder().decode(new TextEncoder().encode(name).slice(0, 300))
    return { videoId: details.videoId, channelId: details.channelId, channelName: safeName, assets: assets.slice(0, 8) }
  } catch {
    return
  }
}

function allowed(): boolean {
  if (!grant || Date.now() >= grant.expires || !sameTarget(grant.target, targetOf(location.href))) return false
  const current = inspect()
  return current?.channelId === grant.channelId && current.assets.some(asset => grant?.assets.includes(asset))
}

function isPlayerMedia(media: HTMLMediaElement): boolean {
  if (!grant || !media.closest('#movie_player, #shorts-player')) return false
  const source = media.getAttribute('src') || media.currentSrc
  const asset = streamAsset(source)
  return asset ? grant.assets.includes(asset) : source.startsWith(`blob:${location.origin}/`)
}

function pause(media: HTMLMediaElement) {
  media.removeAttribute('data-guardian-media')
  if (!media.paused) {
    attempted.add(media)
    originalPause.call(media)
  }
  if (!restoreMute.has(media)) restoreMute.set(media, media.muted)
  if (!media.muted) media.muted = true
  if (media.autoplay) {
    restoreAutoplay.add(media)
    attempted.add(media)
    media.autoplay = false
  }
}

function enforce() {
  const active = allowed()
  if (!active) document.documentElement?.removeAttribute('data-guardian-allowed')
  document.querySelectorAll<HTMLMediaElement>('video, audio').forEach(media => {
    if (active && isPlayerMedia(media)) {
      media.setAttribute('data-guardian-media', '')
      if (restoreMute.has(media)) media.muted = restoreMute.get(media)!
      restoreMute.delete(media)
    } else {
      pause(media)
    }
  })
}

function revoke(reset = false) {
  grant = undefined
  clearTimeout(expiry)
  if (reset) {
    attempted.clear()
    restoreAutoplay.clear()
  }
  enforce()
}

HTMLMediaElement.prototype.play = function(this: HTMLMediaElement) {
  if (!allowed() || !isPlayerMedia(this)) {
    attempted.add(this)
    pause(this)
    return Promise.reject(new DOMException('YouTube Guardian is waiting for approval', 'NotAllowedError'))
  }
  return originalPlay.call(this)
}

for (const event of ['play', 'playing', 'loadedmetadata', 'volumechange', 'loadstart', 'ratechange']) {
  document.addEventListener(event, e => {
    if (e.target instanceof HTMLMediaElement) {
      if (!allowed() || !isPlayerMedia(e.target)) pause(e.target)
      else if (!e.target.hasAttribute('data-guardian-media')) enforce()
    }
  }, true)
}

new MutationObserver(entries => {
  const active = allowed()
  if (!active) {
    for (const entry of entries) {
      if (entry.type === 'attributes' && entry.target instanceof HTMLMediaElement) pause(entry.target)
      for (const node of entry.addedNodes) {
        if (node instanceof HTMLMediaElement) pause(node)
        else if (node instanceof Element) node.querySelectorAll('video, audio').forEach(media => pause(media as HTMLMediaElement))
      }
    }
    document.documentElement?.removeAttribute('data-guardian-allowed')
  } else if (entries.some(entry => entry.target instanceof HTMLMediaElement ||
      [...entry.addedNodes].some(node => node instanceof Element &&
        (node instanceof HTMLMediaElement || node.querySelector('video, audio'))))) {
    enforce()
  }
}).observe(document, { childList: true, subtree: true, attributes: true, attributeFilter: ['autoplay'] })

function navigate() {
  revoke(true)
  document.dispatchEvent(new Event('guardian:navigation'))
}

addEventListener('popstate', navigate, true)
addEventListener('pagehide', navigate, true)
document.addEventListener('yt-navigate-start', navigate, true)
const navigation = (window as Window & { navigation?: EventTarget }).navigation
navigation?.addEventListener('navigate', event => {
  const destination = (event as Event & {destination?: { url: string }}).destination
  if (destination && !sameTarget(targetOf(destination.url), targetOf(location.href))) navigate()
})

document.addEventListener('guardian:inspect', () => {
  document.dispatchEvent(new CustomEvent('guardian:identity', { detail: inspect() }))
})
document.addEventListener('guardian:revoke', () => revoke())
document.addEventListener('guardian:grant', event => {
  const input = (event as CustomEvent).detail
  const identity = inspect()
  const target = targetOf(location.href)
  if (!target || !identity || identity.videoId !== input?.videoId || identity.channelId !== input.channelId
      || !Array.isArray(input.assets) || !identity.assets.some(asset => input.assets.includes(asset))
      || !Number.isFinite(input.expires) || input.expires <= Date.now() || input.expires > Date.now() + leaseMs) return
  const wasAllowed = allowed() && document.documentElement?.hasAttribute('data-guardian-allowed')
  grant = { target, channelId: identity.channelId, assets: input.assets, expires: input.expires }
  clearTimeout(expiry)
  expiry = setTimeout(() => revoke(), Math.max(0, input.expires - Date.now()))
  if (wasAllowed) return

  document.documentElement?.setAttribute('data-guardian-allowed', '')
  enforce()
  for (const media of restoreAutoplay) {
    if (media.isConnected && isPlayerMedia(media)) {
      restoreAutoplay.delete(media)
      media.autoplay = true
    }
  }
  for (const media of attempted) {
    if (!media.isConnected || !isPlayerMedia(media)) continue
    attempted.delete(media)
    // The blocked request can still be pending when the network grant arrives.
    if (media.readyState === HTMLMediaElement.HAVE_NOTHING && streamAsset(media.src)) media.load()
    void originalPlay.call(media).catch(() => undefined)
  }
})

setInterval(() => {
  if (grant) enforce()
  for (const media of restoreMute.keys()) if (!media.isConnected) restoreMute.delete(media)
  for (const media of attempted) if (!media.isConnected) attempted.delete(media)
  for (const media of restoreAutoplay) if (!media.isConnected) restoreAutoplay.delete(media)
}, 150)
