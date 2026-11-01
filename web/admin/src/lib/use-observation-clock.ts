import { useEffect, useState } from 'react'
import { observationClock } from './observed-request'

export function useObservationClock() {
  const [, tick] = useState(0)
  useEffect(() => {
    const update = () => tick(value => value + 1)
    const timer = window.setInterval(update, 1000)
    const events = ['offline', 'online', 'focus', 'pageshow'] as const
    for (const event of events) window.addEventListener(event, update)
    document.addEventListener('visibilitychange', update)
    return () => { window.clearInterval(timer); for (const event of events) window.removeEventListener(event, update); document.removeEventListener('visibilitychange', update) }
  }, [])
  // Read on every render as well as every tick: a response can arrive after sleep
  // before the next interval, and must not inherit an old pre-sleep clock value.
  return { now: observationClock(), available: navigator.onLine && document.visibilityState === 'visible' }
}
