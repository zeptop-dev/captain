import { useEffect, useState } from 'react'
import { readJSON } from '@shared/captain'
import { storageHistoryURL, type StorageHistory, type StorageHistoryRange } from '../../../src/lib/storage-history'

interface Result { key: string; history?: StorageHistory; unsupported?: boolean; error?: string }

/** Selection and time changes immediately discard both cached and in-flight results. */
export function useStorageHistory(uuid: string, selection: string, range: StorageHistoryRange | null, realtime: boolean, enabled: boolean) {
  const url = enabled && range ? storageHistoryURL(uuid, range) : ''
  const key = `${selection}\n${url}`
  const [result, setResult] = useState<Result | null>(null)
  useEffect(() => {
    if (!url) return
    const controller = new AbortController()
    let running = false
    const refresh = async () => {
      if (running) return
      running = true
      try {
        const history = await readJSON<StorageHistory>(url, controller.signal)
        if (!Array.isArray(history.volumes)) throw new Error('Invalid storage history')
        if (!controller.signal.aborted) setResult({ key, history })
      } catch (error) {
        if (!controller.signal.aborted) setResult({ key, unsupported: error instanceof Error && error.message === '404', error: String(error) })
      } finally { running = false }
    }
    void refresh()
    const interval = realtime ? setInterval(() => void refresh(), 30_000) : undefined
    return () => { controller.abort(); clearInterval(interval) }
  }, [url, key, realtime])
  return url && result?.key === key ? result : null
}
