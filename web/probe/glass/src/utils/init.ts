import { appStore } from '@/stores/app'
import { nodesStore } from '@/stores/nodes'
import { snapshot, readJSON, clientFor, statusFor, periodTraffic, shows, clearHistoryCache } from '@shared/captain'
import type { Snapshot } from '@shared/captain'
import type { PublicSettings } from '@shared/utils/api'
import { applyProbeMetadata } from '../../../src/lib/branding'

let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
let stopped = false
let restoreMetadata: (() => void) | undefined
const system = window.matchMedia('(prefers-color-scheme: dark)')

async function refresh(): Promise<boolean> {
  controller?.abort()
  controller = new AbortController()
  try {
    const s = await readJSON<Snapshot>('/api/probe', controller.signal)
    if (stopped) return false
    if (snapshot.value && (s.appearance?.preset !== 'glass' || JSON.stringify(snapshot.value.public_sections) !== JSON.stringify(s.public_sections))) {
      // Clear component-local chart caches when publication permissions change.
      window.location.reload()
      return false
    }
    snapshot.value = s
    const scheme = s.appearance_scheme ?? s.appearance?.scheme ?? 'dark'
    restoreMetadata?.()
    restoreMetadata = applyProbeMetadata(s)
    appStore.setPublicSettings({
      sitename: s.title, description: s.description || '', allow_cors: false, custom_body: '', custom_head: '', disable_password_login: true, oauth_enable: false, oauth_provider: '', theme: 'glass', record_enabled: shows('history'), record_preserve_time: 720,
      ping_record_preserve_time: shows('latency') && shows('history') ? 720 : 0,
      visitor_audit_enabled: false, private_site: false,
      theme_settings: {
        themeMode: scheme === 'auto' ? (system.matches ? 'dark' : 'light') : scheme,
        dataUpdateInterval: s.beat_seconds, visitorInfoEnabled: false, hideEarth: !s.show_globe || !shows('system'),
        defaultViewMode: s.layout === 'compact' ? 'list' : 'card',
        hidePriceWhenLoggedOut: !shows('info'),
        // Costs use public structured billing; legacy price text is display-only.
        generalCardKeys: [...(shows('memory') ? ['memory'] : []), ...(shows('disk') ? ['disk'] : []), ...(shows('info') ? ['remainingValue'] : ['onlineNodes']), ...(shows('traffic') ? ['totalTraffic'] : []), ...(shows('network') ? ['uploadSpeed', 'downloadSpeed'] : [])].join(','),
      },
    } as PublicSettings)
    nodesStore.updateNodeClients(Object.fromEntries(s.nodes.map((n, i) => [String(n.id), clientFor(n, i)])))
    nodesStore.updateNodeStatuses(Object.fromEntries(s.nodes.map(n => [String(n.id), statusFor(n)])))
    // Exact group labels; commas in Captain group names are not separators.
    const byId = new Map(s.nodes.map(v => [String(v.id), v]))
    nodesStore.patchNodes((n) => {
      const source = byId.get(n.uuid)
      if (!source)
        return null
      return { captainTraffic: periodTraffic(source).used, groups: source.group ? [source.group] : [] }
    })
    appStore.setConnectionError(false)
    return true
  } catch (error) {
    if (!stopped && !(error instanceof DOMException && error.name === 'AbortError')) {
      appStore.setConnectionError(true)
      if (error instanceof Error && ['401', '403', '404'].includes(error.message)) {
        snapshot.value = null
        clearHistoryCache()
        nodesStore.clearNodes()
      }
    }
    return false
  } finally {
    appStore.setLoading(false)
    if (!stopped) timer = setTimeout(() => void refresh(), Math.max(5, snapshot.value?.beat_seconds ?? 15) * 1000)
  }
}
function systemChanged() { if ((snapshot.value?.appearance_scheme ?? snapshot.value?.appearance?.scheme) === 'auto') void retryInitApp() }
export async function initApp() { stopped = false; system.addEventListener('change', systemChanged); return refresh() }
export async function retryInitApp() { clearTimeout(timer); return refresh() }
export function destroyInitManager() { stopped = true; clearTimeout(timer); controller?.abort(); system.removeEventListener('change', systemChanged); clearHistoryCache(); restoreMetadata?.(); restoreMetadata = undefined }
