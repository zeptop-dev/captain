import { locateGeo, validCoordinates } from '../../../shared/geolocation.ts'
import type { Location, LocatedPoint, Site } from './site'
import type { StatusEntry } from './service-status'

export function mapLocations(locations: readonly Location[]): LocatedPoint[] {
  return locations.flatMap(location => {
    if (location.precision) {
      const located = locateGeo({ ...location, country: location.country ?? '', precision: location.precision })
      return located ? [{ ...location, ...located }] : []
    }
    // Existing manually configured points keep their exact coordinates.
    return validCoordinates(location.lat, location.lng) ? [{ ...location, lat: location.lat!, lng: location.lng! }] : []
  })
}

/** Accept only the already filtered, visible public entries. No server inventory. */
export function statusMapLocations(entries: readonly StatusEntry[]): LocatedPoint[] {
  const points = entries.flatMap(entry => {
    const location = locateGeo(entry.location)
    return location ? [{ ...location, name: entry.name }] : []
  })
  return points.filter((point, index, all) => all.findIndex(other => other.lat === point.lat && other.lng === point.lng && other.name === point.name) === index)
}

/** New explicit empty/null status settings must not revive homepage entries. */
export function manualStatusMap(site?: Pick<Site, 'locations' | 'hub' | 'status_locations' | 'status_hub'>): { locations: Location[]; hub: Location | null } {
  return {
    locations: site?.status_locations !== undefined ? site.status_locations ?? [] : site?.locations ?? [],
    hub: site?.status_hub !== undefined ? site.status_hub : site?.hub ?? null,
  }
}
