import type { GlobeInstance } from 'globe.gl'
import type { MeshPhongMaterial, Texture } from 'three'
import type { HTMLAttributes } from 'react'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useDocumentVisibility, useElementSize, useElementVisibility } from '@/hooks/useElementObservers'
import { useNodeGeoClusters } from '@/hooks/useNodeGeoClusters'
import { vclass } from '@/lib/vclass'
import '@/scoped/node-earth-realistic-globe.css'
import { scope } from '@/scoped/scope'
import { t, language } from '@shared/i18n'
import { appStore, useApp } from '@/stores/app'
import type { NodeData } from '@/stores/nodes'

const s = scope('node-earth-realistic-globe')

const EARTH_DAY_TEXTURE = './images/earth/earth-blue-marble.jpg'
const EARTH_NIGHT_TEXTURE = './images/earth/earth-night.jpg'
const EARTH_BUMP_MAP = './images/earth/earth-topology.png'
const EARTH_SPECULAR_MAP = './images/earth/earth-water.png'
const CHINA_COORD: [number, number] = [35.8617, 104.1954]

interface GlobePoint { id: string, lat: number, lng: number, code: string, label: string, precision: 'city' | 'country', servers: number, onlineServers: number }
interface GlobeLabel { id: string, lat: number, lng: number, code: string, label: string, precision: 'city' | 'country' }

const countryNames = new Intl.DisplayNames([language], { type: 'region' })
const clusterLabel = (cluster: { code: string; city?: string; subdivision?: string }) => [cluster.city, cluster.subdivision, countryNames.of(cluster.code) || cluster.code].filter(Boolean).join(' · ')

const isDark = () => appStore.getState().isDark
const earthTextureUrl = () => isDark() ? EARTH_NIGHT_TEXTURE : EARTH_DAY_TEXTURE

function pointColor(point: object): string {
  const data = point as GlobePoint
  if (data.onlineServers > 0)
    return isDark() ? 'rgba(34, 211, 238, 1)' : 'rgba(2, 132, 199, 0.98)'
  return isDark() ? 'rgba(250, 204, 21, 0.92)' : 'rgba(202, 138, 4, 0.88)'
}

function createLabelElement(data: object): HTMLElement {
  const label = data as GlobeLabel
  const root = document.createElement('div')
  root.className = 'earth-label'
  root.tabIndex = 0
  root.setAttribute('role', 'img')
  root.dataset.clusterId = label.id
  root.title = `${label.label} · ${t(label.precision === 'city' ? '城市级位置' : '国家级近似位置')}`
  root.setAttribute('aria-label', root.title)
  const flag = document.createElement('img')
  flag.className = 'earth-label-flag'
  flag.src = `./images/flags/${label.code}.svg`
  flag.alt = label.code
  root.appendChild(flag)
  if (label.precision === 'city') {
    root.classList.add('earth-label-city')
    const text = document.createElement('span')
    text.textContent = label.label.split(' · ')[0]
    root.appendChild(text)
  }
  return root
}

export default function NodeEarthRealisticGlobe({ nodes, className, ...attrs }: { nodes?: NodeData[] } & HTMLAttributes<HTMLDivElement>) {
  const dark = useApp(a => a.isDark)
  const stopEarth = useApp(a => a.stopEarth)
  const containerRef = useRef<HTMLDivElement | null>(null)
  const globeHostRef = useRef<HTMLDivElement | null>(null)
  const [globeReady, setGlobeReady] = useState(false)
  const [globeFailed, setGlobeFailed] = useState(false)
  const { width: containerWidth, height: containerHeight } = useElementSize(containerRef)
  const documentVisibility = useDocumentVisibility()
  const elementVisible = useElementVisibility(containerRef)
  const shouldRender = documentVisibility === 'visible' && elementVisible

  const { regionClusters, totalServers, onlineServers, offlineServers, clusterKey } = useNodeGeoClusters(nodes)
  const pointsData = useMemo<GlobePoint[]>(() => regionClusters.map(c => ({ id: c.id, lat: c.coord[0], lng: c.coord[1], code: c.code, label: clusterLabel(c), precision: c.precision, servers: c.servers, onlineServers: c.onlineServers })), [regionClusters])
  const labelsData = useMemo<GlobeLabel[]>(() => regionClusters.map(c => ({ id: c.id, lat: c.coord[0], lng: c.coord[1], code: c.code, label: clusterLabel(c), precision: c.precision })), [regionClusters])

  // Live values for the imperative globe code (Vue read them reactively).
  const live = useRef({ containerWidth, containerHeight, shouldRender, pointsData, labelsData })
  live.current = { containerWidth, containerHeight, shouldRender, pointsData, labelsData }
  const g = useRef<{ globe: GlobeInstance | null, material: MeshPhongMaterial | null, specular: Texture | null, loading: boolean, destroyed: boolean }>({ globe: null, material: null, specular: null, loading: false, destroyed: false })

  function getRenderSize() {
    const width = live.current.containerWidth || globeHostRef.current?.clientWidth || 320
    const height = live.current.containerHeight || globeHostRef.current?.clientHeight || width
    return { width, height }
  }
  function resizeGlobe() {
    const globe = g.current.globe
    if (!globe)
      return
    const { width, height } = getRenderSize()
    globe.width(width).height(height)
  }
  function resetPointOfView(transitionMs = 0) {
    g.current.globe?.pointOfView({ lat: CHINA_COORD[0], lng: CHINA_COORD[1], altitude: 1.95 }, transitionMs)
  }
  function applyControls() {
    const globe = g.current.globe
    if (!globe)
      return
    const controls = globe.controls()
    controls.autoRotate = live.current.shouldRender && !appStore.getState().stopEarth
    controls.autoRotateSpeed = 1.6
    controls.enableDamping = true
    controls.enableZoom = false
    controls.enablePan = false
    controls.rotateSpeed = 0.55
  }
  function applyMaterialStyle() {
    const { globe, material } = g.current
    if (!globe || !material)
      return
    globe.globeImageUrl(earthTextureUrl())
    material.bumpScale = isDark() ? 0.02 : 0.03
    material.shininess = isDark() ? 8 : 14
    material.emissive.set(isDark() ? 0x1E3A5F : 0x355C7D)
    material.emissiveIntensity = isDark() ? 0.48 : 0.32
    material.needsUpdate = true
    globe
      .pointColor(pointColor)
      .ringColor(() => isDark() ? 'rgba(45, 212, 191, 0.34)' : 'rgba(14, 165, 233, 0.28)')
      .atmosphereColor(isDark() ? '#38bdf8' : '#60a5fa')
      .atmosphereAltitude(isDark() ? 0.14 : 0.11)
  }
  function stopGlobe() {
    const globe = g.current.globe
    if (!globe)
      return
    globe.pauseAnimation()
    globe._destructor()
    g.current.globe = null
    g.current.material = null
    g.current.specular?.dispose()
    g.current.specular = null
    globeHostRef.current?.replaceChildren()
  }
  async function startGlobe() {
    if (g.current.globe || g.current.loading || !globeHostRef.current)
      return
    g.current.loading = true
    await Promise.resolve() // nextTick
    try {
      const [{ default: Globe }, THREE] = await Promise.all([import('globe.gl'), import('three')])
      if (g.current.destroyed || !globeHostRef.current)
        return
      const { width, height } = getRenderSize()
      const { pointsData: points, labelsData: labels } = live.current
      const globe = new Globe(globeHostRef.current, {
        // Render a full sphere even when the page pauses during its first frame.
        animateIn: false,
        waitForGlobeReady: false,
        rendererConfig: { alpha: true, antialias: true },
      })
        .width(width)
        .height(height)
        .backgroundColor('rgba(0,0,0,0)')
        .globeImageUrl(earthTextureUrl())
        .bumpImageUrl(EARTH_BUMP_MAP)
        .showAtmosphere(true)
        .pointsData(points)
        .pointLat('lat')
        .pointLng('lng')
        .pointAltitude(0.008)
        .pointRadius((point: object) => ((point as GlobePoint).onlineServers > 0 ? 0.22 : 0.13))
        .pointColor(pointColor)
        .pointLabel((point: object) => {
          const value = point as GlobePoint
          const label = document.createElement('span')
          label.textContent = `${value.label} · ${t(value.precision === 'city' ? '城市级位置' : '国家级近似位置')}`
          return label
        })
        .pointsMerge(false)
        .pointsTransitionDuration(500)
        .ringsData(points)
        .ringLat('lat')
        .ringLng('lng')
        .ringColor(() => isDark() ? 'rgba(45, 212, 191, 0.34)' : 'rgba(14, 165, 233, 0.28)')
        .ringMaxRadius(1.4)
        .ringPropagationSpeed(0.8)
        .ringRepeatPeriod(2400)
        .htmlElementsData(labels)
        .htmlLat('lat')
        .htmlLng('lng')
        .htmlAltitude(0.012)
        .htmlElement(createLabelElement)
        .htmlElementVisibilityModifier((el: HTMLElement, isVisible: boolean) => {
          el.style.opacity = isVisible ? '1' : '0'
          el.style.filter = isVisible ? 'blur(0)' : 'blur(14px)'
        })
        .htmlTransitionDuration(500)
      g.current.globe = globe

      const renderer = globe.renderer()
      renderer.domElement.addEventListener('webglcontextlost', () => { setGlobeFailed(true) })
      renderer.setClearColor(0x000000, 0)
      renderer.domElement.style.background = 'transparent'

      const material = globe.globeMaterial()
      if ('shininess' in material) {
        const phong = material as MeshPhongMaterial
        g.current.material = phong
        g.current.specular = new THREE.TextureLoader().load(EARTH_SPECULAR_MAP, () => {
          if (!g.current.material)
            return
          g.current.material.specularMap = g.current.specular
          g.current.material.needsUpdate = true
        })
        phong.specular = new THREE.Color(isDark() ? 0x64748B : 0x475569)
      }

      const frontLight = new THREE.DirectionalLight(0xFFFFFF, isDark() ? 0.9 : 1.0)
      frontLight.position.set(1.2, 1.1, 1.6)
      const leftFill = new THREE.DirectionalLight(0xDBEAFE, isDark() ? 0.62 : 0.48)
      leftFill.position.set(-1.2, 0.2, 1.2)
      const rearFill = new THREE.DirectionalLight(0xE0F2FE, isDark() ? 0.52 : 0.38)
      rearFill.position.set(-1.0, -0.8, -1.2)
      globe.lights([new THREE.AmbientLight(0xFFFFFF, isDark() ? 1.85 : 1.45), frontLight, leftFill, rearFill])

      applyMaterialStyle()
      applyControls()
      resetPointOfView(0)
      setGlobeReady(true)
      if (!live.current.shouldRender)
        globe.pauseAnimation()
    }
    catch {
      setGlobeFailed(true)
      stopGlobe()
    }
    finally {
      g.current.loading = false
    }
  }

  // onMounted / onBeforeUnmount
  useLayoutEffect(() => {
    g.current.destroyed = false
    void startGlobe()
    return () => {
      g.current.destroyed = true
      stopGlobe()
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // watch([width, height]) (not immediate)
  const firstSize = useRef(true)
  useEffect(() => {
    if (firstSize.current) {
      firstSize.current = false
      return
    }
    if (containerWidth <= 0 || containerHeight <= 0)
      return
    resizeGlobe()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerWidth, containerHeight])

  // watch(clusterKey join) → syncDataToGlobe
  const clusterSignature = regionClusters.map(clusterKey).join(',')
  const firstSig = useRef(true)
  useEffect(() => {
    if (firstSig.current) {
      firstSig.current = false
      return
    }
    g.current.globe?.pointsData(pointsData).ringsData(pointsData).htmlElementsData(labelsData)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterSignature])

  const firstDark = useRef(true)
  useEffect(() => {
    if (firstDark.current) {
      firstDark.current = false
      return
    }
    applyMaterialStyle()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dark])

  const firstStop = useRef(true)
  useEffect(() => {
    if (firstStop.current) {
      firstStop.current = false
      return
    }
    applyControls()
    if (stopEarth)
      resetPointOfView(300)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stopEarth])

  const firstRender = useRef(true)
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false
      return
    }
    const globe = g.current.globe
    if (!globe)
      return
    if (shouldRender) {
      globe.resumeAnimation()
      applyControls()
      resizeGlobe()
      return
    }
    globe.pauseAnimation()
    applyControls()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shouldRender])

  return (
    <div {...attrs} {...s} ref={containerRef} className={vclass('relative z-0 aspect-square w-full max-w-md mx-auto translate-y-2 md:-translate-y-1 overflow-visible pointer-events-none', className)}>
      {(!globeReady || globeFailed) && <div {...s} className="earth-fallback" aria-hidden="true" style={{ backgroundImage: `url(${dark ? EARTH_NIGHT_TEXTURE : EARTH_DAY_TEXTURE})` }} />}
      <div {...s} ref={globeHostRef} style={{ opacity: globeReady && !globeFailed ? 1 : 0 }} className="earth-globe-host absolute inset-0 z-0 w-full h-full scale-106 select-none touch-auto pointer-events-auto cursor-grab active:cursor-grabbing" />

      {totalServers > 0 && (
        <div {...s} className="absolute top-6 md:top-12 left-0 text-[10px] text-muted-foreground pointer-events-none flex gap-2 items-center backdrop-blur-lg bg-background/60 rounded px-2 py-0.5">
          {onlineServers > 0 && (
            <div {...s} className="flex items-center gap-1">
              <span {...s} className="inline-block size-1.5 rounded-full bg-green-600 animate-pulse" />
              <span {...s} className="text-green-600">{onlineServers}</span>
            </div>
          )}
          {offlineServers > 0 && (
            <div {...s} className="flex items-center gap-1">
              <span {...s} className="inline-block size-1.5 rounded-full bg-yellow-600 animate-pulse" />
              <span {...s} className="text-yellow-600">{offlineServers}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
