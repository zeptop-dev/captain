export interface ObservationClock { monotonic: number; wall: number; generation?: number }
export interface ObservedResponse<T> { value: T; started: ObservationClock; received: ObservationClock }
export function createObservationClock(read: () => ObservationClock) {
  let previous: ObservationClock | undefined, generation = 0
  return (): ObservationClock => {
    const next = read()
    // A rollback invalidates previously captured responses permanently. A new
    // request can establish evidence in the new clock generation.
    if (previous && (next.wall < previous.wall || next.monotonic < previous.monotonic)) generation++
    previous = next
    return { ...next, generation }
  }
}
export const observationClock = createObservationClock(() => ({ monotonic: performance.now(), wall: Date.now() }))

// A deadline belongs to request dispatch, so a slow or resumed response cannot
// extend current evidence. Receipt time also guards against later clock rollback.
export function observationFresh(ms: number, started: ObservationClock, received: ObservationClock, now: ObservationClock, available: boolean): boolean {
  if (started.generation !== received.generation || received.generation !== now.generation
      || now.wall < received.wall || now.monotonic < received.monotonic
      || received.wall < started.wall || received.monotonic < started.monotonic) return false
  const elapsed = Math.max(0, now.monotonic - started.monotonic, now.wall - started.wall,
    received.monotonic - started.monotonic, received.wall - started.wall)
  return available && Number.isFinite(ms) && ms > elapsed
}

export async function observedRequest<T>(signal: AbortSignal, read: (signal: AbortSignal) => Promise<T>, timeoutMS = 12000): Promise<ObservedResponse<T>> {
  const controller = new AbortController(), started = observationClock()
  const abort = () => controller.abort(signal.reason)
  signal.addEventListener('abort', abort, { once: true })
  if (signal.aborted) abort()
  const timer = setTimeout(() => controller.abort(new Error('observation_timeout')), timeoutMS)
  try {
    controller.signal.throwIfAborted()
    const value = await read(controller.signal)
    controller.signal.throwIfAborted()
    return { value, started, received: observationClock() }
  } finally { clearTimeout(timer); signal.removeEventListener('abort', abort) }
}
