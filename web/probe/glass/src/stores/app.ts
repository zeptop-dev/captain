// React counterpart of glass/src/stores/app.ts. Derivations come from the
// shared deriveAppSettings() so both builds compute identical values; the
// localStorage keys and formats match the Vue build's useStorageAsync.
import { useMemo } from 'react'
import { language } from '@shared/i18n'
import type { AuthStatus, PermissionKey, VerifyLoginOptions } from '@shared/services/auth.service'
import { getAuthSession, requirePermission, setAuthSessionFromLogin, verifyLogin } from '@shared/services/auth.service'
import {
  BYTE_DECIMALS,
  deriveAppSettings,
  getBeijingHour,
  isValidManagedThemeMode,
  isValidThemeMode,
  normalizeThemeSettings,
} from '@shared/stores/app-settings'
import type {
  DerivedAppSettings,
  Lang,
  ManagedThemeMode,
  NodeViewMode,
  ThemeMode,
  ThemeSettings,
} from '@shared/stores/app-settings'
import type { MeInfo, PublicSettings } from '@shared/utils/api'
import type { ByteDecimalsConfig } from '@shared/utils/helper'
import { createStore, shallowEqual, useSelector } from './store'
import { onStoredChange, readStored, writeStored } from './storage'

export type {
  ThemeMode,
  ManagedThemeMode,
  GeneralCardKey,
  HomeQuickControlKey,
  DetailMetricCardKey,
  NodeListMetadataField,
  ChartDashboardCardKey,
  ChartDashboardTemplate,
} from '@shared/stores/app-settings'
export type { GlassCustomColors } from '@shared/types/theme'

const KEY_THEME_MODE = 'themeMode'
const KEY_GROUP = 'nodeSelectedGroup'
const KEY_FAVORITES = 'theme:favorite-nodes:v1'
const KEY_VIEW_MODE = 'nodeViewMode'

function isValidViewMode(value: string | null): value is NodeViewMode {
  return value === 'card' || value === 'list'
}

interface AppRawState {
  loading: boolean
  themeMode: ThemeMode
  lang: Lang
  publicSettings: PublicSettings | undefined
  nodeSelectedGroup: string
  favoriteNodeIds: string[]
  isLoggedIn: boolean
  authStatus: AuthStatus
  connectionError: boolean
  homeAdvancedToolsVisible: boolean
  homeScrollPosition: number
  storedViewMode: NodeViewMode | null
  beijingTimeTick: number
}

const session = getAuthSession()
const raw = createStore<AppRawState>({
  loading: true,
  themeMode: readStored<ThemeMode>(KEY_THEME_MODE, 'auto', 'string'),
  lang: language,
  publicSettings: undefined,
  nodeSelectedGroup: readStored<string>(KEY_GROUP, 'all', 'string'),
  favoriteNodeIds: readStored<string[]>(KEY_FAVORITES, [], 'json'),
  isLoggedIn: session.authenticated,
  authStatus: session.status,
  connectionError: false,
  homeAdvancedToolsVisible: false,
  homeScrollPosition: 0,
  storedViewMode: readStored<NodeViewMode | null>(KEY_VIEW_MODE, null, 'string'),
  beijingTimeTick: Date.now(),
})

onStoredChange<ThemeMode>(KEY_THEME_MODE, 'auto', 'string', v => raw.setState({ themeMode: v }))
onStoredChange<string>(KEY_GROUP, 'all', 'string', v => raw.setState({ nodeSelectedGroup: v }))
onStoredChange<string[]>(KEY_FAVORITES, [], 'json', v => raw.setState({ favoriteNodeIds: v }))
onStoredChange<NodeViewMode | null>(KEY_VIEW_MODE, null, 'string', v => raw.setState({ storedViewMode: v }))

if (typeof window !== 'undefined') {
  window.setInterval(() => {
    raw.setState({ beijingTimeTick: Date.now() })
  }, 60 * 1000)
}

// Persisted fields write through on every change, like useStorageAsync.
function persist(prev: AppRawState, next: AppRawState): void {
  if (prev.themeMode !== next.themeMode)
    writeStored(KEY_THEME_MODE, next.themeMode, 'string')
  if (prev.nodeSelectedGroup !== next.nodeSelectedGroup)
    writeStored(KEY_GROUP, next.nodeSelectedGroup, 'string')
  if (prev.favoriteNodeIds !== next.favoriteNodeIds)
    writeStored(KEY_FAVORITES, next.favoriteNodeIds, 'json')
  if (prev.storedViewMode !== next.storedViewMode)
    writeStored(KEY_VIEW_MODE, next.storedViewMode, 'string')
}

export interface AppState extends DerivedAppSettings {
  loading: boolean
  themeMode: ThemeMode
  managedThemeMode: ManagedThemeMode
  isBeijingDaytime: boolean
  isDark: boolean
  resolvedThemeMode: 'light' | 'dark'
  lang: Lang
  nodeSelectedGroup: string
  favoriteNodeIds: string[]
  favoriteNodeIdSet: Set<string>
  nodeViewMode: NodeViewMode
  byteDecimals: ByteDecimalsConfig
  homeAdvancedToolsVisible: boolean
  visitorAuditSupported: boolean
  visitorAuditEnabled: boolean
  currentBackgroundUrl: string
  isLoggedIn: boolean
  authStatus: AuthStatus
  privateFeaturesAllowed: boolean
  publicSettings: PublicSettings | undefined
  connectionError: boolean
  homeScrollPosition: number
  themeSettings: ThemeSettings
}

const byteDecimals: ByteDecimalsConfig = { ...BYTE_DECIMALS }

// Memoisation mirrors Vue's computed graph: each layer recomputes only when
// its own inputs change, so selected values keep their identity.
let lastPublicSettings: PublicSettings | undefined | null = null
let lastThemeSettings: ThemeSettings = normalizeThemeSettings(undefined)
let lastDerived: DerivedAppSettings = deriveAppSettings(lastThemeSettings)
let lastFavoriteIds: string[] | null = null
let lastFavoriteSet = new Set<string>()
let lastRaw: AppRawState | null = null
let lastState: AppState | null = null

function derive(state: AppRawState): AppState {
  if (lastRaw === state && lastState)
    return lastState
  if (state.publicSettings !== lastPublicSettings) {
    lastPublicSettings = state.publicSettings
    const themeSettings = normalizeThemeSettings(state.publicSettings?.theme_settings)
    if (themeSettings !== lastThemeSettings) {
      lastThemeSettings = themeSettings
      lastDerived = deriveAppSettings(themeSettings)
    }
  }
  if (state.favoriteNodeIds !== lastFavoriteIds) {
    lastFavoriteIds = state.favoriteNodeIds
    lastFavoriteSet = new Set(
      (Array.isArray(state.favoriteNodeIds) ? state.favoriteNodeIds : [])
        .filter((id): id is string => typeof id === 'string' && Boolean(id.trim())),
    )
  }
  const derived = lastDerived
  const managedThemeMode: ManagedThemeMode = isValidManagedThemeMode(lastThemeSettings.themeMode) ? lastThemeSettings.themeMode : 'beijing'
  const hour = getBeijingHour(state.beijingTimeTick)
  const isBeijingDaytime = hour >= 7 && hour < 19
  // 本机按钮选择 auto 时跟随后台托管设置；手动选择浅色/深色时仅覆盖当前浏览器。
  const localMode = isValidThemeMode(state.themeMode) ? state.themeMode : 'auto'
  const isDark = localMode === 'light'
    ? false
    : localMode === 'dark'
      ? true
      : managedThemeMode === 'beijing' ? !isBeijingDaytime : managedThemeMode === 'dark'
  const resolvedThemeMode = isDark ? 'dark' : 'light'
  const nodeViewMode = state.storedViewMode !== null && isValidViewMode(state.storedViewMode)
    ? state.storedViewMode
    : derived.defaultViewMode
  const next: AppState = {
    ...derived,
    loading: state.loading,
    themeMode: state.themeMode,
    managedThemeMode,
    isBeijingDaytime,
    isDark,
    resolvedThemeMode,
    lang: state.lang,
    nodeSelectedGroup: state.nodeSelectedGroup,
    favoriteNodeIds: state.favoriteNodeIds,
    favoriteNodeIdSet: lastFavoriteSet,
    nodeViewMode,
    byteDecimals,
    homeAdvancedToolsVisible: state.homeAdvancedToolsVisible,
    visitorAuditSupported: typeof state.publicSettings?.visitor_audit_enabled === 'boolean',
    visitorAuditEnabled: state.publicSettings?.visitor_audit_enabled === true,
    currentBackgroundUrl: resolvedThemeMode === 'dark' ? derived.darkBackgroundUrl : derived.lightBackgroundUrl,
    isLoggedIn: state.isLoggedIn,
    authStatus: state.authStatus,
    privateFeaturesAllowed: state.authStatus === 'authenticated',
    publicSettings: state.publicSettings,
    connectionError: state.connectionError,
    homeScrollPosition: state.homeScrollPosition,
    themeSettings: lastThemeSettings,
  }
  lastRaw = state
  lastState = next
  return next
}

function setRaw(patch: Partial<AppRawState>): void {
  const prev = raw.getState()
  raw.setState(patch)
  const next = raw.getState()
  if (next !== prev)
    persist(prev, next)
}

// Same post-load fix-ups as the Vue store's immediate watchers.
function normalise(): void {
  const s = raw.getState()
  if (!isValidThemeMode(s.themeMode))
    setRaw({ themeMode: 'auto' })
  if (s.publicSettings && !isValidViewMode(s.storedViewMode))
    setRaw({ storedViewMode: derive(raw.getState()).defaultViewMode })
}
normalise()

function syncAuthState() {
  const current = getAuthSession()
  setRaw({ isLoggedIn: current.authenticated, authStatus: current.status })
  return current
}

export const appStore = {
  getState: (): AppState => derive(raw.getState()),
  subscribe: raw.subscribe,

  setLoading(loading: boolean) { setRaw({ loading }) },
  setPublicSettings(publicSettings: PublicSettings | undefined) {
    setRaw({ publicSettings })
    normalise()
  },
  setConnectionError(connectionError: boolean) { setRaw({ connectionError }) },
  setNodeSelectedGroup(nodeSelectedGroup: string) { setRaw({ nodeSelectedGroup }) },
  setNodeViewMode(mode: NodeViewMode) { setRaw({ storedViewMode: mode }) },
  setHomeScrollPosition(homeScrollPosition: number) { setRaw({ homeScrollPosition }) },
  setHomeAdvancedToolsVisible(homeAdvancedToolsVisible: boolean) { setRaw({ homeAdvancedToolsVisible }) },

  isFavoriteNode(uuid: string): boolean {
    return derive(raw.getState()).favoriteNodeIdSet.has(uuid)
  },
  toggleFavoriteNode(uuid: string): void {
    const normalized = uuid.trim()
    if (!normalized)
      return
    const set = derive(raw.getState()).favoriteNodeIdSet
    setRaw({
      favoriteNodeIds: set.has(normalized)
        ? [...set].filter(id => id !== normalized)
        : [...set, normalized],
    })
  },

  updateThemeMode(mode?: ThemeMode) {
    if (mode) {
      setRaw({ themeMode: isValidThemeMode(mode) ? mode : 'auto' })
      return
    }
    const nextMode: Record<ThemeMode, ThemeMode> = { auto: 'light', light: 'dark', dark: 'auto' }
    const current = raw.getState().themeMode
    setRaw({ themeMode: nextMode[isValidThemeMode(current) ? current : 'auto'] })
  },
  updateLoginState(loggedIn: boolean, user?: MeInfo | null) {
    setAuthSessionFromLogin(loggedIn, user ?? null)
    syncAuthState()
  },
  async verifyLoginState(options?: VerifyLoginOptions): Promise<boolean> {
    await verifyLogin(options)
    return syncAuthState().authenticated
  },
  async requireLoginPermission(permission: PermissionKey, options?: VerifyLoginOptions): Promise<boolean> {
    const result = await requirePermission(permission, options)
    syncAuthState()
    return result.granted
  },
}

const readApp = () => appStore.getState()

/** Selects one value (or a shallow-equal object of values) from the app store. */
export function useApp<S>(select: (state: AppState) => S): S {
  return useSelector(appStore.subscribe, readApp, select, shallowEqual)
}

/** The whole derived state; re-renders on any change. */
export function useAppState(): AppState {
  return useSelector(appStore.subscribe, readApp, s => s)
}

/** Stable action bag, for components that only call methods. */
export function useAppActions() {
  return useMemo(() => appStore, [])
}
