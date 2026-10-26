export type PortalScheme = 'light' | 'dark'
export const portalSchemeStorageKey = 'captain-portal-color-scheme'

export function parsePortalPreference(value: string | null): PortalScheme | null {
  return value === 'light' || value === 'dark' ? value : null
}

export function readPortalPreference(storage: Pick<Storage, 'getItem'>): PortalScheme | null {
  try { return parsePortalPreference(storage.getItem(portalSchemeStorageKey)) } catch { return null }
}

export function effectivePortalScheme(preference: PortalScheme | null, configured: string | undefined, systemDark: boolean, fallback: PortalScheme = 'light'): PortalScheme {
  if (preference) return preference
  if (configured === 'auto') return systemDark ? 'dark' : 'light'
  return configured === 'dark' || configured === 'light' ? configured : fallback
}
