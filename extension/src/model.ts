export const leaseMs = 4500
export const pollMs = 1600
export const channelPattern = /^UC[A-Za-z0-9_-]{22}$/
export const videoPattern = /^[A-Za-z0-9_-]{11}$/
export const assetPattern = /^[A-Za-z0-9._~-]{1,120}$/
export const tokenPattern = /^[a-fA-F0-9]{64}$/

export type Target = { id: string; kind: 'watch' | 'embed' | 'shorts' | 'live' }
export type Identity = { videoId: string; channelId: string; channelName: string; assets: string[] }
export type Decision = 'unknown' | 'approved' | 'blocked' | 'unavailable' | 'unresolved' | 'unsupported' | 'shorts'
export type Check = { decision: Decision; version: number | null; blockShorts: boolean; expires?: number; pending?: boolean }

export function supportedHost(hostname: string): boolean {
  return ['www.youtube.com', 'm.youtube.com', 'www.youtube-nocookie.com'].includes(hostname)
}

export function targetOf(href: string): Target | undefined {
  const url = new URL(href)
  if (url.protocol !== 'https:' || !supportedHost(url.hostname)) return
  if (url.pathname === '/watch') {
    const id = url.searchParams.get('v')
    if (id && videoPattern.test(id)) return { id, kind: 'watch' }
    return
  }
  const match = /^\/(shorts|embed|live)\/([A-Za-z0-9_-]{11})\/?$/.exec(url.pathname)
  if (match) return { id: match[2], kind: match[1] as Target['kind'] }
}

export function sameTarget(a?: Target, b?: Target): boolean {
  return a?.id === b?.id && a?.kind === b?.kind
}

export function validIdentity(value: unknown): value is Identity {
  const data = value as Partial<Identity> | null
  return !!data && typeof data.videoId === 'string' && videoPattern.test(data.videoId)
    && typeof data.channelId === 'string' && channelPattern.test(data.channelId)
    && typeof data.channelName === 'string' && new TextEncoder().encode(data.channelName).length <= 320
    && Array.isArray(data.assets) && data.assets.length <= 8
    && data.assets.every(asset => typeof asset === 'string' && assetPattern.test(asset))
}
