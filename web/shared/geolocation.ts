import { COORD_MAP } from './country-coordinates.ts'

/** Public display metadata only: no lookup address, provider or timestamps. */
export interface GeoLocation {
  country: string
  subdivision?: string
  city?: string
  lat?: number
  lng?: number
  precision: 'city' | 'country'
}

export interface LocatedGeo extends GeoLocation { lat: number; lng: number }

export function validCoordinates(lat: unknown, lng: unknown): boolean {
  return typeof lat === 'number' && Number.isFinite(lat) && Math.abs(lat) <= 90
    && typeof lng === 'number' && Number.isFinite(lng) && Math.abs(lng) <= 180
}

export function locateGeo(location: GeoLocation | null | undefined): LocatedGeo | null {
  if (!location || typeof location.country !== 'string') return null
  const country = location.country.trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(country)) return null
  if (location.precision === 'city') {
    if (typeof location.city !== 'string' || !location.city.trim() || !validCoordinates(location.lat, location.lng)) return null
    return { ...location, country, city: location.city.trim(), lat: location.lat!, lng: location.lng! }
  }
  if (location.precision !== 'country' || !Object.hasOwn(COORD_MAP, country)) return null
  const [lat, lng] = COORD_MAP[country]
  // Country-only results must not retain a stale city or pretend 0,0 is known.
  return { country, precision: 'country', lat, lng }
}

export function geoKey(location: LocatedGeo): string {
  return location.precision === 'country' ? `country:${location.country}`
    : JSON.stringify(['city', location.country, location.subdivision ?? '', location.city ?? '', location.lat, location.lng])
}
