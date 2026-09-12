import { useEffect, useRef } from 'react'
import Globe from 'globe.gl'
import { feature } from 'topojson-client'
import type { Location } from '../lib/site'

// A dotted dark globe with the node locations and arcs from the hub.
// Textures would need a CDN, so land is drawn from the bundled 110m atlas.
export function NodeGlobe({ locations, hub }: { locations: Location[]; hub: { name: string; lat: number; lng: number } | null }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const size = el.clientWidth
    const globe = new Globe(el, { animateIn: true })
      .width(size).height(size)
      .backgroundColor('rgba(0,0,0,0)')
      .showAtmosphere(true).atmosphereColor('#22d3ee').atmosphereAltitude(0.18)
      .globeMaterial(Object.assign(globe_material(), {}))
    globe.controls().autoRotate = true
    globe.controls().autoRotateSpeed = 0.6
    globe.controls().enableZoom = false
    import('world-atlas/countries-110m.json').then((mod) => {
      const atlas = (mod as { default?: unknown }).default ?? mod
      const topo = atlas as unknown as { objects: { countries: never } }
      const countries = feature(topo as never, topo.objects.countries) as unknown as { features: object[] }
      // h3 chokes on the polar polygon; nobody has a node there anyway.
      globe.hexPolygonsData(countries.features.filter((f) => (f as { properties?: { name?: string } }).properties?.name !== 'Antarctica')).hexPolygonResolution(3).hexPolygonMargin(0.55).hexPolygonUseDots(true).hexPolygonColor(() => 'rgba(148, 163, 184, 0.55)')
    })
    const points = locations.map((l) => ({ ...l, size: 0.35 }))
    globe.pointsData(points).pointAltitude(0.02).pointRadius(0.45).pointColor(() => '#67e8f9').pointLabel((d) => `<b>${(d as Location).name}</b>`)
    globe.ringsData(points).ringColor(() => (t: number) => `rgba(103, 232, 249, ${1 - t})`).ringMaxRadius(3).ringPropagationSpeed(1.2).ringRepeatPeriod(1400)
    if (hub && locations.length) {
      const arcs = locations.map((l) => ({ startLat: hub.lat, startLng: hub.lng, endLat: l.lat, endLng: l.lng }))
      globe.arcsData(arcs).arcColor(() => ['rgba(165,180,252,0.1)', '#67e8f9']).arcAltitudeAutoScale(0.35).arcStroke(0.5).arcDashLength(0.5).arcDashGap(0.6).arcDashAnimateTime(2400)
    }
    const first = hub ?? locations[0]
    if (first) globe.pointOfView({ lat: first.lat + 10, lng: first.lng, altitude: 2.1 }, 0)
    const onResize = () => { const s = el.clientWidth; globe.width(s).height(s) }
    window.addEventListener('resize', onResize)
    return () => { window.removeEventListener('resize', onResize); globe._destructor() }
  }, [locations, hub])
  return <div className="globe-wrap"><div className="globe-glow" /><div ref={ref} /></div>
}

// Dark ocean without a texture.
function globe_material() {
  // globe.gl exposes three's MeshPhongMaterial through globeMaterial(); a
  // plain object with the fields we want is merged into it.
  return { color: { r: 0.03, g: 0.05, b: 0.1, isColor: true } as unknown as never, emissive: { r: 0.02, g: 0.06, b: 0.1, isColor: true } as unknown as never, shininess: 4 } as unknown as never
}
