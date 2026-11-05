export interface AddressFallback {
  address?: string
  source: 'captain_cloudflare_trace'
  origin: string
  sampled_at: number
  received_at: number
  error?: 'timeout' | 'network' | 'tls' | 'http_status' | 'invalid_response' | 'invalid_origin'
}
export function addressObservationFresh(receivedAt: number | undefined, now: number): boolean {
  return !!receivedAt && Number.isFinite(receivedAt) && now >= receivedAt && now - receivedAt < 900
}
export function addressFallbackError(error: string): NonNullable<AddressFallback['error']> {
  return (['timeout', 'network', 'tls', 'http_status', 'invalid_response', 'invalid_origin'] as const).find(value => value === error) ?? 'invalid_response'
}
