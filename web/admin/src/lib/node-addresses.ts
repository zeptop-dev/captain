export interface AddressFallback {
  address?: string
  source: 'captain_cloudflare_trace'
  origin: string
  sampled_at: number
  received_at: number
  check_status?: 'no_conflict' | 'conflict'
  run_id?: string
  error?: 'timeout' | 'network' | 'tls' | 'http_status' | 'invalid_response' | 'invalid_origin'
}
export function addressObservationFresh(receivedAt: number | undefined, now: number): boolean {
  return !!receivedAt && Number.isFinite(receivedAt) && now >= receivedAt && now - receivedAt < 900
}
// A delayed upload cannot grant a fresh full lifetime to an old sample.
export function addressFallbackFresh(fallback: AddressFallback | undefined, now: number): boolean {
  return !!fallback?.address && !fallback.error && addressObservationFresh(fallback.received_at, now)
    && Number.isFinite(fallback.sampled_at) && fallback.sampled_at > 0
    && now - fallback.sampled_at < 900 && fallback.sampled_at - now <= 60
}
export function addressFallbackError(error: string): NonNullable<AddressFallback['error']> {
  return (['timeout', 'network', 'tls', 'http_status', 'invalid_response', 'invalid_origin'] as const).find(value => value === error) ?? 'invalid_response'
}

export interface AddressFallbackFollow {
  address?: string
  source?: string
  origin?: string
  run_id?: string
  candidate_since?: number
  candidate_sampled_at?: number
  last_sampled_at?: number
  last_success?: number
  confirmed_at?: number
}

export type FallbackFollowState = 'disabled' | 'primary' | 'unavailable' | 'legacy' | 'conflict' | 'waiting' | 'confirming' | 'applying' | 'following'

// Match the current evidence as well as the saved policy: a historical
// confirmation must not make a new address, endpoint or process look confirmed.
export function fallbackFollowState({ enabled, fresh, primary, fallback, candidate, entry }: {
  enabled: boolean
  fresh: boolean
  primary: boolean
  fallback?: AddressFallback
  candidate?: AddressFallbackFollow
  entry: string
}): FallbackFollowState {
  if (!enabled) return 'disabled'
  if (primary) return 'primary'
  if (!fresh || !fallback?.address || fallback.error) return 'unavailable'
  if (fallback.check_status === 'conflict') return 'conflict'
  if (fallback.check_status !== 'no_conflict' || !/^[a-f0-9]{32}$/.test(fallback.run_id ?? '')) return 'legacy'
  if (!candidate?.candidate_since || candidate.address !== fallback.address || candidate.origin !== fallback.origin || candidate.source !== fallback.source || candidate.run_id !== fallback.run_id || candidate.last_sampled_at !== fallback.sampled_at || candidate.last_success !== fallback.received_at) return 'waiting'
  if (!candidate.confirmed_at) return 'confirming'
  if (candidate.confirmed_at > fallback.received_at || candidate.confirmed_at < candidate.candidate_since) return 'waiting'
  return entry === fallback.address ? 'following' : 'applying'
}
