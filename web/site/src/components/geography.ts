import { feature } from 'topojson-client'
import atlas from 'world-atlas/land-110m.json'
import type { Location } from '../lib/site'
export { mapLocations as validLocations } from '../lib/map-locations'

type Coordinate = [number, number]
type Polygon = Coordinate[][]
type LandFeatureCollection = { features: { geometry: { type: 'MultiPolygon'; coordinates: Polygon[] } }[] }

// Bundled geometry: no map tiles, geolocation request or third-party images.
const land = feature(atlas as never, atlas.objects.land as never) as unknown as LandFeatureCollection
const polygons = land.features.flatMap((item) => item.geometry.coordinates).map((polygon) => polygon.map(unwrapRing))

function unwrapRing(ring: Coordinate[]): Coordinate[] {
  let previous = ring[0]?.[0] ?? 0
  return ring.map(([longitude, latitude]) => {
    while (longitude - previous > 180) longitude -= 360
    while (longitude - previous < -180) longitude += 360
    previous = longitude
    return [longitude, latitude]
  })
}

const indexedPolygons = polygons.map((polygon) => polygon.map((ring) => ({ ring, minX: Math.min(...ring.map(([x]) => x)), maxX: Math.max(...ring.map(([x]) => x)), minY: Math.min(...ring.map(([, y]) => y)), maxY: Math.max(...ring.map(([, y]) => y)) })))
function insideRing(longitude: number, latitude: number, bounds: typeof indexedPolygons[number][number]): boolean {
  const { ring, minX, maxX, minY, maxY } = bounds
  if (latitude < minY || latitude > maxY) return false
  longitude += 360 * Math.round(((minX + maxX) / 2 - longitude) / 360)
  if (longitude < minX || longitude > maxX) return false
  let inside = false
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const [xi, yi] = ring[i], [xj, yj] = ring[j]
    if ((yi > latitude) !== (yj > latitude) && longitude < (xj - xi) * (latitude - yi) / (yj - yi) + xi) inside = !inside
  }
  return inside
}

// Even geographic spacing avoids clumping at the poles. Only the projection
// changes during animation; the geographic samples are prepared once.
export const landDots: Coordinate[] = []
for (let latitude = -58; latitude <= 82; latitude += 2.1) {
  const spacing = 2.1 / Math.max(0.2, Math.cos(latitude * Math.PI / 180))
  for (let longitude = -180; longitude < 180; longitude += spacing) {
    if (indexedPolygons.some((polygon) => insideRing(longitude, latitude, polygon[0]) && !polygon.slice(1).some((hole) => insideRing(longitude, latitude, hole)))) landDots.push([longitude, latitude])
  }
}

export function projectFlat(longitude: number, latitude: number): Coordinate {
  // Use one scale on both axes: shortening latitude to fit a banner distorts
  // the map. The viewport handles layout without changing geographic shapes.
  const scale = 1070 / 360
  return [40 + (longitude + 180) * scale, 28 + (90 - latitude) * scale]
}

export const flatLandPath = [-360, 0, 360].map((shift) => polygons.map((polygon) => polygon.map((ring) => {
  if (ring.every(([, latitude]) => latitude < -60)) return ''
  return ring.map(([longitude, latitude], index) => {
    const [x, y] = projectFlat(longitude + shift, latitude)
    return `${index ? 'L' : 'M'}${x.toFixed(2)},${y.toFixed(2)}`
  }).join('') + 'Z'
}).join('')).join('')).join('')

export function locationLabel(location: Location): string {
  const name = location.tag?.trim() || location.city?.trim() || location.name
  return location.precision === 'country' ? `≈ ${name}` : name
}
