import type { ResourceHost } from './resources'

// Live beats and full reports arrive independently. Prefer the actual resource
// sample time; for legacy samples without a timestamp, retain the live fallback.
export function latestResourceHost(live?: ResourceHost | null, reported?: ResourceHost | null): ResourceHost | null | undefined {
  return (live?.resources?.at ?? 0) >= (reported?.resources?.at ?? 0) ? live ?? reported : reported
}
