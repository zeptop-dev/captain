import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useComputedColorScheme } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { Location } from '../lib/site'
import { landDots, locationLabel, validLocations } from './geography'
import { globePalette } from './geography-theme'
import styles from './Globe.module.css'

interface Props { locations: Location[]; hub: Location | null; variant?: 'status' | 'home' }
const radians = Math.PI / 180

export function NodeGlobe({ locations, hub, variant = 'home' }: Props) {
  const { t, i18n } = useTranslation()
  const scheme = useComputedColorScheme('dark')
  const canvas = useRef<HTMLCanvasElement>(null)
  const host = useRef<HTMLDivElement>(null)
  const pausedRef = useRef(false)
  const syncAnimation = useRef<(() => void) | null>(null)
  const [paused, setPaused] = useState(false)
  const [available, setAvailable] = useState(true)
  const description = useId()
  const points = useMemo(() => validLocations(hub ? [...locations, hub] : locations).filter((point, index, all) => all.findIndex((other) => other.lat === point.lat && other.lng === point.lng && other.name === point.name) === index), [locations, hub])
  const locationNames = new Intl.ListFormat(i18n.resolvedLanguage || i18n.language || 'en', { type: 'conjunction' }).format(points.map((point) => point.name))

  useEffect(() => { pausedRef.current = paused; syncAnimation.current?.() }, [paused])
  useEffect(() => {
    const element = host.current, surface = canvas.current
    if (!element || !surface) return
    const context = surface.getContext('2d')
    if (!context) { setAvailable(false); return }
    const palette = globePalette(scheme)
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)')
    let reduced = motion.matches
    const origin = hub && validLocations([hub])[0] || points[0]
    let width = 470, frame = 0, last = 0, longitude = origin?.lng ?? 100
    const latitude = Math.max(-35, Math.min(45, origin?.lat ?? 22)) * radians
    const sinLat = Math.sin(latitude), cosLat = Math.cos(latitude)
    let visible = true

    const render = () => {
      const center = width / 2, radius = width * 0.488
      context.clearRect(0, 0, width, width)
      const ocean = context.createRadialGradient(center - radius * 0.28, center - radius * 0.32, radius * 0.12, center, center, radius)
      ocean.addColorStop(0, palette.ocean[0]); ocean.addColorStop(0.75, palette.ocean[1]); ocean.addColorStop(1, palette.ocean[2])
      context.beginPath(); context.arc(center, center, radius, 0, Math.PI * 2)
      context.fillStyle = ocean; context.fill()
      context.strokeStyle = palette.border; context.lineWidth = 1; context.stroke()
      const project = (lng: number, lat: number) => {
        const angle = (lng - longitude) * radians, phi = lat * radians
        const cos = Math.cos(phi), z = sinLat * Math.sin(phi) + cosLat * cos * Math.cos(angle)
        return { x: center + radius * cos * Math.sin(angle), y: center - radius * (cosLat * Math.sin(phi) - sinLat * cos * Math.cos(angle)), z }
      }
      for (const [lng, lat] of landDots) {
        const point = project(lng, lat)
        if (point.z <= 0.015) continue
        context.beginPath(); context.arc(point.x, point.y, Math.max(0.55, width / 470 * (0.65 + point.z * 0.55)), 0, Math.PI * 2)
        context.fillStyle = `rgba(${palette.land}, ${0.2 + Math.pow(point.z, 0.45) * 0.78})`; context.fill()
      }
      const occupied: { x: number; y: number; width: number }[] = []
      for (const location of points) {
        const point = project(location.lng, location.lat)
        if (point.z <= 0.12) continue
        const accent = variant === 'status' ? palette.marker : palette.homeMarker
        context.beginPath(); context.arc(point.x, point.y, 3.1, 0, Math.PI * 2)
        context.fillStyle = accent; context.fill()
        context.strokeStyle = variant === 'status' ? palette.markerRing : palette.homeRing
        context.beginPath(); context.arc(point.x, point.y, 6, 0, Math.PI * 2); context.stroke()
        const label = locationLabel(location).slice(0, 22)
        context.font = `${Math.max(9, width / 470 * 10)}px ui-monospace, SFMono-Regular, monospace`
        const textWidth = context.measureText(label).width + 13
        const x = Math.min(width - textWidth - 4, point.x + 9), y = point.y - 17
        if (occupied.some((previous) => Math.abs(previous.y - y) < 21 && x < previous.x + previous.width + 5 && x + textWidth > previous.x - 5)) continue
        occupied.push({ x, y, width: textWidth })
        context.fillStyle = palette.labelBackground; context.strokeStyle = palette.labelBorder
        context.beginPath(); context.roundRect(x, y, textWidth, 20, 10); context.fill(); context.stroke()
        context.fillStyle = palette.label; context.fillText(label, x + 6, y + 13.4)
      }
    }
    const resize = () => {
      width = Math.max(1, element.clientWidth)
      const ratio = Math.min(window.devicePixelRatio || 1, 2)
      surface.width = Math.round(width * ratio); surface.height = Math.round(width * ratio)
      context.setTransform(ratio, 0, 0, ratio, 0, 0)
      render()
    }
    const tick = (now: number) => {
      frame = 0
      if (!shouldAnimate()) return
      if (last) longitude += Math.min(now - last, 80) * 0.0008
      render()
      last = now
      frame = window.requestAnimationFrame(tick)
    }
    const shouldAnimate = () => !document.hidden && visible && !reduced && !pausedRef.current
    const sync = () => {
      last = 0
      if (shouldAnimate()) { if (!frame) frame = window.requestAnimationFrame(tick) }
      else { window.cancelAnimationFrame(frame); frame = 0 }
    }
    syncAnimation.current = sync
    const onMotion = () => { reduced = motion.matches; render(); sync() }
    const observer = new ResizeObserver(resize)
    observer.observe(element)
    const intersection = new IntersectionObserver(([entry]) => { visible = entry.isIntersecting; sync() })
    intersection.observe(element)
    motion.addEventListener('change', onMotion)
    document.addEventListener('visibilitychange', sync)
    resize(); sync()
    return () => { syncAnimation.current = null; window.cancelAnimationFrame(frame); observer.disconnect(); intersection.disconnect(); motion.removeEventListener('change', onMotion); document.removeEventListener('visibilitychange', sync) }
  }, [points, hub, variant, scheme])

  return <div ref={host} className={styles.globe} data-variant={variant}>
    <canvas ref={canvas} role="img" aria-label={t('geography.globeTitle')} aria-describedby={description} />
    {!available && <div className={styles.fallback}>{t('geography.unavailable')}</div>}
    <p id={description} className={styles.srOnly}>{t(points.length ? 'geography.configuredLocations' : 'geography.noLocations', { names: locationNames })} {t('geography.globeDisclaimer')}</p>
    <button type="button" className={styles.control} onClick={() => setPaused((value) => !value)} aria-pressed={paused} aria-label={t(paused ? 'geography.resumeGlobe' : 'geography.pauseGlobe')}>{t(paused ? 'geography.play' : 'geography.pause')}<span aria-hidden="true">{paused ? ' ▷' : ' Ⅱ'}</span></button>
  </div>
}
