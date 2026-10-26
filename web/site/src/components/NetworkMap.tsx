import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Location } from '../lib/site'
import { flatLandPath, locationLabel, projectFlat, validLocations } from './geography'
import styles from './NetworkMap.module.css'

export function NetworkMap({ locations, hub }: { locations: Location[]; hub: Location | null }) {
  const { t, i18n } = useTranslation()
  const id = useId().replace(/:/g, '')
  const [paused, setPaused] = useState(false)
  const points = useMemo(() => validLocations(locations), [locations])
  const origin = hub && validLocations([hub])[0]
  const displayPoints = origin && !points.some((point) => point.lat === origin.lat && point.lng === origin.lng) ? [...points, origin] : points
  const locationNames = new Intl.ListFormat(i18n.resolvedLanguage || i18n.language || 'en', { type: 'conjunction' }).format(displayPoints.map((point) => point.name))
  const arcs = origin ? points.filter((point) => point.lat !== origin.lat || point.lng !== origin.lng).map((point) => {
    const [x1, y1] = projectFlat(origin.lng, origin.lat), [x2, y2] = projectFlat(point.lng, point.lat)
    // Wrap across the antimeridian instead of drawing a nearly full-world arc.
    const wrappedX = Math.abs(x2 - x1) > 535 ? x2 + (x2 > x1 ? -1070 : 1070) : x2
    const lift = Math.min(72, Math.abs(wrappedX - x1) * 0.16 + Math.abs(y2 - y1) * 0.2 + 12)
    return [-1070, 0, 1070].map((shift) => `M${x1 + shift},${y1} Q${(x1 + wrappedX) / 2 + shift},${Math.min(y1, y2) - lift} ${wrappedX + shift},${y2}`)
  }).flat() : []
  return <div className={styles.map} data-paused={paused}>
    <div className={styles.viewport} tabIndex={0} role="region" aria-label={t('geography.mapViewport')}>
    <svg viewBox="0 0 1150 590" preserveAspectRatio="xMidYMid meet" role="img" aria-labelledby={`${id}-title ${id}-description`}>
      <title id={`${id}-title`}>{t('geography.mapTitle')}</title>
      <desc id={`${id}-description`}>{t(displayPoints.length ? 'geography.configuredLocations' : 'geography.noLocations', { names: locationNames })} {t('geography.mapDisclaimer')}</desc>
      <defs><pattern id={`${id}-grid`} width="46" height="46" patternUnits="userSpaceOnUse"><path d="M46 0H0V46" fill="none" stroke="var(--map-grid)" strokeOpacity=".035" strokeWidth=".6" /></pattern><clipPath id={`${id}-clip`}><rect x="40" y="12" width="1070" height="566" /></clipPath></defs>
      <rect width="1150" height="590" fill={`url(#${id}-grid)`} />
      {/* Enlarge the geography uniformly and place its center slightly below
          the frame center to balance the legend and northern land mass.
          Move the longitude clip, markers and routes with the land. */}
      <g transform="matrix(1.1 0 0 1.1 -57.5 27.671861)">
      <g clipPath={`url(#${id}-clip)`}><path d={flatLandPath} fill="var(--map-land)" fillOpacity=".67" stroke="var(--map-outline)" strokeOpacity=".08" strokeWidth=".6" fillRule="evenodd" />
        <g fill="none" stroke="var(--map-accent)" strokeWidth=".9" strokeOpacity=".45" strokeDasharray="2 5">{arcs.map((arc, index) => <path key={index} d={arc} className={styles.arc} />)}</g>
        {displayPoints.map((point, index) => {
          const [x, y] = projectFlat(point.lng, point.lat)
          return <g key={`${point.name}-${index}`} transform={`translate(${x},${y})`} className={styles.location} tabIndex={0} role="img" aria-label={`${t('geography.location', { name: point.name })}${point.precision ? ` · ${t(point.precision === 'country' ? 'geography.countryApproximate' : 'geography.cityLocation')}` : ''}`}>
            <title>{point.name}{point.city ? ` · ${point.city}` : ''}{point.precision ? ` · ${t(point.precision === 'country' ? 'geography.countryApproximate' : 'geography.cityLocation')}` : ''}</title><circle r="15" className={styles.ring} style={{ animationDelay: `${index % 5 * -0.65}s` }} /><circle r="6" fill="var(--map-accent)" fillOpacity=".09" stroke="var(--map-accent)" strokeOpacity=".5" strokeWidth=".7" /><circle r="2.4" fill="var(--map-marker)" />
            <g className={styles.label} transform={`translate(${x > 950 ? -12 : 12},-9)`}><rect x={x > 950 ? -132 : 0} y="-12" width="132" height="25" rx="4" fill="var(--map-label-bg)" stroke="var(--map-label-line)" /><text x={x > 950 ? -124 : 8} y="4" fill="var(--map-label-text)" fontSize="11" fontFamily="ui-monospace, monospace">{locationLabel(point).slice(0, 18)}</text></g>
          </g>
        })}
      </g>
      </g>
    </svg>
    </div>
    <span className={styles.legend}><i aria-hidden="true" /> {t('geography.illustration')}{points.some(point => point.precision === 'country') ? ` · ≈ ${t('geography.countryApproximate')}` : ''}</span>
    <span className={styles.scrollHint}>{t('geography.scrollHint')}</span>
    <button type="button" className={styles.control} onClick={() => setPaused((value) => !value)} aria-pressed={paused}>{t(paused ? 'geography.playAnimation' : 'geography.pauseAnimation')}<span aria-hidden="true">{paused ? ' ▷' : ' Ⅱ'}</span></button>
  </div>
}
