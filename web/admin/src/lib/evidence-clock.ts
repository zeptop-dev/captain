import { useEffect, useState } from 'react'
import { agentHTTPSFresh } from './agent-https'
export function useEvidenceClock() {
 const [clock, setClock] = useState(() => ({ wall: Date.now(), mono: performance.now(), online: navigator.onLine, changedAt: 0 }))
 useEffect(() => {
  const tick = () => setClock(old => ({ ...old, wall: Date.now(), mono: performance.now(), online: navigator.onLine }))
  const changed = () => setClock({ wall: Date.now(), mono: performance.now(), online: navigator.onLine, changedAt: Date.now() })
  const timer = setInterval(tick, 1000); window.addEventListener('offline', changed); window.addEventListener('online', changed)
  return () => { clearInterval(timer); window.removeEventListener('offline', changed); window.removeEventListener('online', changed) }
 }, [])
 return clock
}
export function evidenceFresh(start: { wall: number; mono: number } | undefined, clock: { wall: number; mono: number; online: boolean; changedAt: number }) {
 return !!start && clock.online && start.wall >= clock.changedAt && agentHTTPSFresh(start.wall, start.mono, clock.wall, clock.mono)
}
