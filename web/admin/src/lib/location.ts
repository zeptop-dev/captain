export interface GeoLocation {
  country: string
  subdivision?: string
  city?: string
  lat?: number
  lng?: number
  precision: 'city' | 'country'
}
export interface LocationSettings { mode: 'auto' | 'manual' | 'off'; manual?: GeoLocation }
export interface LocationDraft {
  mode: LocationSettings['mode']
  country: string
  subdivision: string
  city: string
  lat: number | string
  lng: number | string
}

export function locationDraft(settings?: LocationSettings | null): LocationDraft {
  return {
    mode: settings?.mode || 'auto', country: settings?.manual?.country || '',
    subdivision: settings?.manual?.subdivision || '', city: settings?.manual?.city || '',
    lat: settings?.manual?.lat ?? '', lng: settings?.manual?.lng ?? '',
  }
}

// Older servers have no separate map mode. Preserve their explicit region opt-out.
export function nodeLocationDraft(settings: LocationSettings | null | undefined, regionMode: string): LocationDraft {
  return locationDraft(settings ?? { mode: regionMode === 'off' ? 'off' : 'auto' })
}

export function manualLocationError(draft: LocationDraft): 'countryRequired' | 'cityCoordinatesRequired' | 'invalidCoordinates' | null {
  if (!/^[A-Z]{2}$/.test(draft.country.trim().toUpperCase())) return 'countryRequired'
  const hasLat = draft.lat !== '', hasLng = draft.lng !== ''
  if (draft.city.trim() || hasLat || hasLng) {
    if (!draft.city.trim() || !hasLat || !hasLng) return 'cityCoordinatesRequired'
    const lat = Number(draft.lat), lng = Number(draft.lng)
    if (!Number.isFinite(lat) || !Number.isFinite(lng) || lat < -90 || lat > 90 || lng < -180 || lng > 180) return 'invalidCoordinates'
  }
  return null
}

export function locationDraftError(draft: LocationDraft) {
  return draft.mode === 'manual' ? manualLocationError(draft) : null
}

export function locationSettings(draft: LocationDraft): LocationSettings {
  const settings: LocationSettings = { mode: draft.mode }
  // Retain a complete manual correction when switching back to automatic or off.
  if (!manualLocationError(draft)) settings.manual = {
    country: draft.country.trim().toUpperCase(), subdivision: draft.subdivision.trim() || undefined,
    city: draft.city.trim() || undefined, precision: draft.city.trim() ? 'city' : 'country',
    ...(draft.city.trim() ? { lat: Number(draft.lat), lng: Number(draft.lng) } : {}),
  }
  return settings
}

export function locationName(location?: GeoLocation | null): string {
  return location ? [location.city, location.subdivision, location.country].filter(Boolean).join(' · ') : ''
}
