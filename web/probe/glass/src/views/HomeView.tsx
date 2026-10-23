import type { CSSProperties, Ref } from 'react'
import { asyncComponent } from '@/lib/asyncComponent'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from '@iconify/react'
import { t } from '@shared/i18n'
import type { PermissionKey } from '@shared/services/auth.service'
import { UI_CONFIG } from '@shared/constants/ui'
import * as financeHelper from '@shared/utils/financeHelper'
import { getRealtimePeakSpeed, getTotalTraffic, isExpiringNode, isHighLoadNode } from '@shared/utils/nodeMetricsHelper'
import { isNodeMatchSearch } from '@shared/utils/nodeSearch'
import { isFreeNode } from '@shared/utils/tagHelper'
import DeferredRender from '@/components/DeferredRender'
import MarkdownRenderer from '@/components/MarkdownRenderer'
import TransitionGroup from '@/components/TransitionGroup'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Empty } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useVisitorAudit } from '@/hooks/useVisitorAudit'
import { useActivation, useRouter } from '@/lib/router'
import '@/scoped/home-view.css'
import { scope } from '@/scoped/scope'
import type { HomeQuickControlKey } from '@/stores/app'
import { appStore, useApp } from '@/stores/app'
import type { NodeData } from '@/stores/nodes'
import { useNodes } from '@/stores/nodes'

const AuditLogPanel = asyncComponent(() => import('@/components/AuditLogPanel'))
const HealthSummaryPanel = asyncComponent(() => import('@/components/HealthSummaryPanel'))
const NodeCard = asyncComponent(() => import('@/components/NodeCard'))
const NodeGeneralCards = asyncComponent(() => import('@/components/NodeGeneralCards'))
const NodeList = asyncComponent(() => import('@/components/NodeList'))
const NodeComparePanel = asyncComponent(() => import('@/components/NodeComparePanel'))
const PingMonitorDialog = asyncComponent(() => import('@/components/PingMonitorDialog'))
const NodeTopologyPanel = asyncComponent(() => import('@/components/NodeTopologyPanel'))
const ProviderValuePanel = asyncComponent(() => import('@/components/ProviderValuePanel'))
const SnapshotExportPanel = asyncComponent(() => import('@/components/SnapshotExportPanel'))

const s = scope('home-view')

interface QuickControlOption {
  key: HomeQuickControlKey
  label: string
  icon: string
}

type HomeToolKey = 'nodes' | 'nodeCompare' | 'topology' | 'providerValue' | 'healthSummary' | 'snapshotExport' | 'auditLog'
type PrivateHomeToolKey = Exclude<HomeToolKey, 'nodes' | 'nodeCompare'>

interface HomeToolOption {
  key: Exclude<HomeToolKey, 'nodes'>
  label: string
  icon: string
  description: string
}

const nodeItemStaggerMs = UI_CONFIG.motion.staggerMs
const nodeItemStaggerLimit = UI_CONFIG.motion.staggerLimit
const denseNodeAppearThreshold = UI_CONFIG.motion.denseNodeAppearThreshold
const denseNodePingAnimationThreshold = UI_CONFIG.motion.denseNodePingAnimationThreshold

const homeToolPermissionMap: Record<PrivateHomeToolKey, PermissionKey> = {
  topology: 'nodeTopology',
  providerValue: 'providerValue',
  healthSummary: 'healthSummary',
  snapshotExport: 'snapshotExport',
  auditLog: 'auditLog',
}

function quickControlDefinitions(): Record<HomeQuickControlKey, QuickControlOption> {
  return {
    favorite: { key: 'favorite', label: t('收藏'), icon: 'tabler:star' },
    monthlyCost: { key: 'monthlyCost', label: '月成本', icon: 'tabler:calendar-dollar' },
    totalTraffic: { key: 'totalTraffic', label: t('总流量'), icon: 'tabler:database' },
    upload: { key: 'upload', label: '上行', icon: 'tabler:chevron-up' },
    download: { key: 'download', label: '下行', icon: 'tabler:chevron-down' },
    peak: { key: 'peak', label: t('峰值'), icon: 'tabler:activity' },
    offline: { key: 'offline', label: t('离线'), icon: 'tabler:plug-connected-x' },
    highLoad: { key: 'highLoad', label: t('高负载'), icon: 'tabler:alert-triangle' },
    expiring: { key: 'expiring', label: t('即将到期'), icon: 'tabler:calendar-exclamation' },
  }
}

function sortNodesByComputedValue(nodes: NodeData[], selector: (node: NodeData) => number): NodeData[] {
  return nodes
    .map(node => ({ node, value: selector(node) }))
    .sort((a, b) => b.value - a.value)
    .map(item => item.node)
}

/** Wrapper element of one card; forwards its ref so TransitionGroup can animate it. */
function NodeCardItem({ ref, style, children }: { ref?: Ref<HTMLDivElement>, style: CSSProperties, children: React.ReactNode }) {
  return <div ref={ref} {...s} className="min-w-0" style={style}>{children}</div>
}

export default function HomeView({ ref }: { ref?: Ref<HTMLDivElement> }) {
  const router = useRouter()
  const { record: recordVisitorEvent } = useVisitorAudit()
  const app = useApp(a => ({
    disablePageAnimation: a.disablePageAnimation,
    alertEnabled: a.alertEnabled,
    alertContent: a.alertContent,
    alertTitle: a.alertTitle,
    hideGeneralCard: a.hideGeneralCard,
    nodeSelectedGroup: a.nodeSelectedGroup,
    homeToolsEnabled: a.homeToolsEnabled,
    privateFeaturesAllowed: a.privateFeaturesAllowed,
    homeQuickControlOrder: a.homeQuickControlOrder,
    homeQuickControlsEnabled: a.homeQuickControlsEnabled,
    homeAdvancedToolsVisible: a.homeAdvancedToolsVisible,
    nodeViewMode: a.nodeViewMode,
    nodeCardSize: a.nodeCardSize,
    offlineNodesLast: a.offlineNodesLast,
    homeHighLoadThreshold: a.homeHighLoadThreshold,
    homeExpiringDays: a.homeExpiringDays,
    favoriteNodeIdSet: a.favoriteNodeIdSet,
  }))
  const visibleNodes = useNodes(n => n.visibleNodes)
  const nodeGroups = useNodes(n => n.groups)

  const [isViewActive, setIsViewActive] = useState(true)
  useActivation(() => {
    setIsViewActive(true)
    queueMicrotask(() => {
      const top = appStore.getState().homeScrollPosition
      if (top > 0)
        window.scrollTo({ top, behavior: 'instant' })
    })
  }, () => {
    setIsViewActive(false)
    appStore.setHomeScrollPosition(window.scrollY)
  })

  const [searchText, setSearchText] = useState('')
  const [debouncedSearchText, setDebouncedSearchText] = useState('')
  const [activeHomeTool, setActiveHomeTool] = useState<HomeToolKey>('nodes')
  const [activeQuickControl, setActiveQuickControl] = useState<HomeQuickControlKey | null>(null)
  const [exchangeRates, setExchangeRates] = useState(financeHelper.DEFAULT_EXCHANGE_RATES)
  const [excludeFreeNodes, setExcludeFreeNodes] = useState(true)
  const [pingDialogNode, setPingDialogNode] = useState<NodeData | null>(null)

  const homeTools = useMemo<HomeToolOption[]>(() => {
    if (!app.homeToolsEnabled)
      return []
    const tools: HomeToolOption[] = [
      { key: 'nodeCompare', label: '对比', icon: 'tabler:columns-3', description: '最多四台节点实时横向对比' },
    ]
    if (!app.privateFeaturesAllowed)
      return tools
    return [...tools, { key: 'topology', label: '拓扑', icon: 'tabler:route', description: 'ASN / BGP / 上游根因' }, { key: 'providerValue', label: '性价比', icon: 'tabler:scale', description: '单机资源成本对比' }, { key: 'healthSummary', label: '健康', icon: 'tabler:heartbeat', description: '日周月历史健康概览' }, { key: 'snapshotExport', label: '导出', icon: 'tabler:download', description: 'CSV / JSON 数据快照' }, { key: 'auditLog', label: '日志', icon: 'tabler:list-details', description: '管理员操作审计日志' }]
  }, [app.homeToolsEnabled, app.privateFeaturesAllowed])

  // useDebounceFn(…, 300): trailing call after 300 ms of quiet.
  useEffect(() => {
    const id = setTimeout(() => setDebouncedSearchText(searchText), 300)
    return () => clearTimeout(id)
  }, [searchText])

  const groups = useMemo(() => [
    { tab: t('全部节点'), name: 'all' },
    ...nodeGroups.map(g => ({ tab: g, name: g })),
  ], [nodeGroups])

  const quickControlKeys = useMemo<HomeQuickControlKey[]>(() => app.homeQuickControlOrder.filter(key => key !== 'monthlyCost'), [app.homeQuickControlOrder])

  // Immediate watcher in Vue: clamp during render so the first paint matches.
  const quickOrderKey = `${app.homeQuickControlOrder.join('|')}:${app.homeQuickControlsEnabled}`
  const lastQuickOrderKey = useRef<string | null>(null)
  if (lastQuickOrderKey.current !== quickOrderKey) {
    lastQuickOrderKey.current = quickOrderKey
    if (!app.homeQuickControlsEnabled) {
      if (activeQuickControl !== null)
        setActiveQuickControl(null)
    }
    else if (activeQuickControl && !quickControlKeys.includes(activeQuickControl)) {
      setActiveQuickControl(null)
    }
  }

  useEffect(() => {
    let cancelled = false
    setExcludeFreeNodes(financeHelper.shouldExcludeFreeNodes())
    void financeHelper.getDailyExchangeRates().then(({ rates }) => {
      if (!cancelled)
        setExchangeRates(rates)
    })
    return () => {
      cancelled = true
    }
  }, [])

  // Immediate watcher on nodesStore.groups.
  useEffect(() => {
    const cur = appStore.getState().nodeSelectedGroup
    if (cur !== 'all' && !nodeGroups.includes(cur))
      appStore.setNodeSelectedGroup('all')
  }, [nodeGroups])

  const groupNodeList = useMemo(() => {
    const selectedGroup = app.nodeSelectedGroup
    if (selectedGroup === 'all')
      return visibleNodes
    return visibleNodes.filter(node => node.groups.includes(selectedGroup))
  }, [app.nodeSelectedGroup, visibleNodes])

  const nodeList = useMemo(() => {
    const getNodeMonthlyCostCNY = (node: NodeData): number => {
      if (excludeFreeNodes && isFreeNode(node))
        return 0
      return financeHelper.calculateMonthlyCostCNY(node, exchangeRates)
    }
    const placeOfflineNodesLast = (nodes: NodeData[]): NodeData[] => {
      if (!app.offlineNodesLast)
        return nodes
      return [...nodes].sort((a, b) => {
        if (a.online === b.online)
          return 0
        return a.online ? -1 : 1
      })
    }
    let filtered = groupNodeList
    if (debouncedSearchText.trim())
      filtered = filtered.filter(n => isNodeMatchSearch(n, debouncedSearchText))
    let result: NodeData[]
    switch (activeQuickControl) {
      case 'favorite':
        return filtered.filter(node => app.favoriteNodeIdSet.has(node.uuid))
      case 'monthlyCost':
        result = sortNodesByComputedValue(filtered, getNodeMonthlyCostCNY)
        break
      case 'totalTraffic':
        result = sortNodesByComputedValue(filtered, getTotalTraffic)
        break
      case 'upload':
        result = [...filtered].sort((a, b) => (b.net_out || 0) - (a.net_out || 0))
        break
      case 'download':
        result = [...filtered].sort((a, b) => (b.net_in || 0) - (a.net_in || 0))
        break
      case 'peak':
        result = sortNodesByComputedValue(filtered, getRealtimePeakSpeed)
        break
      case 'offline':
        return filtered.filter(node => !node.online)
      case 'highLoad':
        result = filtered.filter(node => isHighLoadNode(node, app.homeHighLoadThreshold))
        break
      case 'expiring':
        result = filtered.filter(node => isExpiringNode(node, app.homeExpiringDays))
        break
      default:
        result = filtered
        break
    }
    return placeOfflineNodesLast(result)
  }, [groupNodeList, debouncedSearchText, activeQuickControl, app.favoriteNodeIdSet, app.offlineNodesLast, app.homeHighLoadThreshold, app.homeExpiringDays, excludeFreeNodes, exchangeRates])

  const isDenseNodeGrid = app.nodeViewMode === 'card' && nodeList.length > denseNodeAppearThreshold
  const enableNodeCardTransition = !app.disablePageAnimation && !isDenseNodeGrid
  const reduceDenseNodeEffects = app.nodeViewMode === 'card' && nodeList.length > denseNodePingAnimationThreshold
  const deferNodeCards = app.nodeViewMode === 'card' && nodeList.length > UI_CONFIG.virtualList.nodeThreshold
  const deferredNodeCardHeight = ({ mini: 220, compact: 270, comfortable: 310, large: 350 } as const)[app.nodeCardSize]

  const quickControlCounts = useMemo<Record<HomeQuickControlKey, number>>(() => {
    let base = groupNodeList
    if (debouncedSearchText.trim())
      base = base.filter(n => isNodeMatchSearch(n, debouncedSearchText))
    const counts = {} as Record<HomeQuickControlKey, number>
    for (const key of quickControlKeys) {
      switch (key) {
        case 'favorite':
          counts[key] = base.reduce((count, node) => count + (app.favoriteNodeIdSet.has(node.uuid) ? 1 : 0), 0)
          break
        case 'offline':
          counts[key] = base.reduce((count, node) => count + (node.online ? 0 : 1), 0)
          break
        case 'highLoad':
          counts[key] = base.reduce((count, node) => count + (isHighLoadNode(node, app.homeHighLoadThreshold) ? 1 : 0), 0)
          break
        case 'expiring':
          counts[key] = base.reduce((count, node) => count + (isExpiringNode(node, app.homeExpiringDays) ? 1 : 0), 0)
          break
        default:
          counts[key] = base.length
      }
    }
    return counts
  }, [groupNodeList, debouncedSearchText, quickControlKeys, app.favoriteNodeIdSet, app.homeHighLoadThreshold, app.homeExpiringDays])

  const quickControls = useMemo(() => {
    const defs = quickControlDefinitions()
    return quickControlKeys.filter(key => key !== 'favorite' || quickControlCounts.favorite > 0).map(key => defs[key])
  }, [quickControlKeys, quickControlCounts])
  const showQuickControls = app.homeQuickControlsEnabled && quickControls.length > 0
  // The home view stays mounted during detail navigation. Removing the last
  // matching favorite must not leave an invisible filter active on return.
  if (activeQuickControl === 'favorite' && !(quickControlCounts.favorite > 0))
    setActiveQuickControl(null)

  const emptyDescription = debouncedSearchText.trim()
    ? '没有匹配的节点'
    : activeQuickControl
      ? '当前快捷筛选下暂无节点'
      : t('暂无节点')

  function clearSearch() {
    setSearchText('')
    setDebouncedSearchText('')
  }

  const nodeListSortResetKey = `${app.nodeSelectedGroup}|${debouncedSearchText.trim()}|${activeQuickControl ?? 'all'}`

  function handleNodeClick(node: NodeData) {
    void router.push({ name: 'instance-detail', params: { id: node.uuid } })
  }

  function openPingDialog(node: NodeData) {
    setPingDialogNode(node)
  }

  function getNodeItemTransitionKey(node: NodeData): string {
    return `${app.nodeSelectedGroup}-${activeQuickControl ?? 'all'}-${node.uuid}`
  }

  function getNodeItemTransitionStyle(index: number): CSSProperties {
    return { '--node-item-delay': `${Math.min(index, nodeItemStaggerLimit) * nodeItemStaggerMs}ms` } as CSSProperties
  }

  function setQuickControl(key: HomeQuickControlKey) {
    const next = activeQuickControl === key ? null : key
    setActiveQuickControl(next)
    void recordVisitorEvent({
      event: 'filter_change',
      path: '/',
      route: 'home',
      target: next ?? 'all',
      detail: { active: Boolean(next), result_count: nodeList.length },
    })
  }

  function setNodeViewMode(mode: 'card' | 'list') {
    if (app.nodeViewMode === mode)
      return
    appStore.setNodeViewMode(mode)
    void recordVisitorEvent({ event: 'view_mode_change', path: '/', route: 'home', target: mode })
  }

  async function toggleHomeTool(key: Exclude<HomeToolKey, 'nodes'>) {
    if (!homeTools.some(tool => tool.key === key))
      return
    if (activeHomeTool === key) {
      setActiveHomeTool('nodes')
      return
    }
    const permission = key === 'nodeCompare' ? null : homeToolPermissionMap[key]
    if (permission) {
      const granted = await appStore.requireLoginPermission(permission, { force: true })
      if (!granted) {
        setActiveHomeTool('nodes')
        window.$message?.warning('登录状态已过期，请重新登录后使用高级工具。')
        return
      }
    }
    setActiveHomeTool(key)
    void recordVisitorEvent({ event: 'home_tool_open', path: '/', route: 'home', target: key })
  }

  // Immediate watcher on homeTools; clamp during render.
  if (activeHomeTool !== 'nodes' && !homeTools.some(tool => tool.key === activeHomeTool))
    setActiveHomeTool('nodes')

  const prevAdvancedVisible = useRef(app.homeAdvancedToolsVisible)
  useEffect(() => {
    if (prevAdvancedVisible.current !== app.homeAdvancedToolsVisible && !app.homeAdvancedToolsVisible)
      setActiveHomeTool('nodes')
    prevAdvancedVisible.current = app.homeAdvancedToolsVisible
  }, [app.homeAdvancedToolsVisible])

  const prevGroup = useRef(app.nodeSelectedGroup)
  useEffect(() => {
    const previous = prevGroup.current
    prevGroup.current = app.nodeSelectedGroup
    if (app.nodeSelectedGroup === previous)
      return
    void recordVisitorEvent({ event: 'group_change', path: '/', route: 'home', target: app.nodeSelectedGroup, detail: { visible_nodes: groupNodeList.length } })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app.nodeSelectedGroup])

  const prevDebounced = useRef(debouncedSearchText)
  useEffect(() => {
    const previous = prevDebounced.current
    prevDebounced.current = debouncedSearchText
    const keyword = debouncedSearchText.trim()
    if (keyword === previous.trim())
      return
    void recordVisitorEvent({ event: keyword ? 'search' : 'search_clear', path: '/', route: 'home', detail: { keyword_length: keyword.length, result_count: nodeList.length } })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearchText])

  const activeToolTitle = activeHomeTool === 'nodes' ? '' : homeTools.find(tool => tool.key === activeHomeTool)?.description ?? ''

  const nodeCardGridClass = `grid grid-cols-1 ${({
    mini: 'gap-3 sm:grid-cols-[repeat(auto-fill,minmax(270px,1fr))]',
    compact: 'gap-3 sm:grid-cols-[repeat(auto-fill,minmax(300px,1fr))]',
    comfortable: 'gap-4 sm:grid-cols-[repeat(auto-fill,minmax(360px,1fr))]',
    large: 'gap-5 sm:grid-cols-[repeat(auto-fill,minmax(420px,1fr))]',
  } as const)[app.nodeCardSize]}`

  return (
    <div ref={ref} {...s} className={`home-view${!app.disablePageAnimation ? ' home-view--motion' : ''}`}>
      {app.alertEnabled && app.alertContent && (
        <div {...s} className="alert px-4">
          <Alert {...s} className="border-none bg-background/60 backdrop-blur-xs rounded-md">
            {app.alertTitle && (
              <AlertTitle {...s}>
                {app.alertTitle}
              </AlertTitle>
            )}
            <AlertDescription {...s}>
              <MarkdownRenderer {...s} content={app.alertContent} />
            </AlertDescription>
          </Alert>
        </div>
      )}

      {!app.hideGeneralCard && (
        <>
          <NodeGeneralCards
            {...s}
            nodes={groupNodeList}
            globeNodes={groupNodeList}
            transitionKey={app.nodeSelectedGroup}
          />
        </>
      )}

      <div {...s} className={`node-info p-4 pt-0 flex flex-col gap-4 relative z-1 pointer-events-none${app.hideGeneralCard ? ' pt-4' : ''}`}>
        <div {...s} className="nodes min-w-0">
          <Tabs {...s} value={app.nodeSelectedGroup} onValueChange={v => appStore.setNodeSelectedGroup(v)} className="w-full flex-col gap-4">
            <div {...s} className="flex flex-col gap-2 xl:flex-row xl:items-center">
              <div {...s} className="home-controls-scroll min-w-0 overflow-x-auto overscroll-x-contain rounded-sm pointer-events-auto touch-pan-x xl:flex-1">
                <div {...s} className="flex w-max gap-2">
                  <TabsList {...s} className="w-max h-8 bg-background/50 backdrop-blur-xl rounded-md pointer-events-auto">
                    {groups.map(g => (
                      <TabsTrigger
                        {...s}
                        key={g.name}
                        value={g.name}
                        className="h-6.5 flex-none shrink-0 text-xs border-none data-[state=active]:text-selection shadow-none rounded-sm"
                      >
                        {g.tab}
                      </TabsTrigger>
                    ))}
                  </TabsList>

                  {showQuickControls && activeHomeTool === 'nodes' && (
                    <div {...s} className="flex h-8 w-max items-center gap-1 rounded-md bg-background/50 px-1 backdrop-blur-xl pointer-events-auto">
                      {quickControls.map(control => (
                        <button
                          {...s}
                          key={control.key}
                          data-quick-control={control.key}
                          type="button"
                          className={`inline-flex h-6.5 flex-none shrink-0 items-center gap-1 rounded-sm px-2 text-xs text-muted-foreground transition-colors hover:text-foreground${activeQuickControl === control.key ? ' bg-background text-selection shadow-sm' : ''}`}
                          aria-pressed={activeQuickControl === control.key}
                          aria-label={`切换到${control.label}节点，${quickControlCounts[control.key] ?? 0} 台`}
                          onClick={() => setQuickControl(control.key)}
                        >
                          <Icon {...s} icon={control.icon} width={12} height={12} />
                          <span {...s}>{control.label}</span>
                          <span {...s} className="rounded-full bg-slate-500/10 px-1 text-[10px] tabular-nums text-foreground/65">
                            {quickControlCounts[control.key] ?? 0}
                          </span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              </div>
              <div {...s} className="search flex min-w-0 max-w-full flex-wrap gap-2 items-center justify-end pointer-events-auto max-sm:justify-start xl:ml-auto xl:shrink-0">
                {homeTools.length > 0 && app.homeAdvancedToolsVisible && (
                  <div {...s} className="flex h-8 items-center gap-1 rounded-md bg-background/50 p-0.5 backdrop-blur-xs">
                    {homeTools.map(tool => (
                      <Button
                        {...s}
                        key={tool.key}
                        variant="ghost"
                        size="icon"
                        className={`size-7 rounded-sm text-muted-foreground shadow-none hover:bg-background/60${activeHomeTool === tool.key ? ' !text-selection !bg-background' : ''}`}
                        aria-label={`${tool.label}：${tool.description}`}
                        aria-pressed={activeHomeTool === tool.key}
                        title={tool.description}
                        onClick={() => void toggleHomeTool(tool.key)}
                      >
                        <Icon {...s} icon={tool.icon} width={14} height={14} />
                      </Button>
                    ))}
                  </div>
                )}

                <Button
                  {...s}
                  variant="outline"
                  size="icon"
                  aria-label={t('卡片视图')}
                  className={`w-8 h-8 border-none bg-background/50 backdrop-blur-xs shadow-none hover:bg-background/60 rounded-md${app.nodeViewMode === 'card' ? ' !text-selection !bg-background' : ''}`}
                  onClick={() => setNodeViewMode('card')}
                >
                  <Icon {...s} icon="tabler:layout-grid" width={14} height={14} />
                </Button>
                <Button
                  {...s}
                  variant="outline"
                  size="icon"
                  aria-label={t('列表视图')}
                  className={`w-8 h-8 border-none bg-background/50 backdrop-blur-xs shadow-none hover:bg-background/60 rounded-md${app.nodeViewMode === 'list' ? ' !text-selection !bg-background' : ''}`}
                  onClick={() => setNodeViewMode('list')}
                >
                  <Icon {...s} icon="tabler:table" width={14} height={14} />
                </Button>
                <div {...s} className={`relative z-1 h-8 max-w-full shrink-0 ${searchText ? 'w-52 sm:w-60' : 'w-8 focus-within:w-52 sm:focus-within:w-60'}`}>
                  <div {...s} className="relative h-full w-full">
                    <Input
                      {...s}
                      value={searchText}
                      onValueChange={v => setSearchText(String(v))}
                      placeholder="搜索名称、地区、IP、CPU"
                      aria-label={t('搜索节点')}
                      className={`transition-all border-none shadow-none h-8 bg-background/50 backdrop-blur-xs rounded-md hover:!bg-background/60 focus:!pl-7.5 focus:placeholder:!text-muted-foreground focus:!bg-background/80 focus:!ring-slate-500/10 ${searchText ? '!w-full !pl-7.5 pr-7 placeholder:!text-muted-foreground' : '!w-full placeholder:text-transparent'}`}
                      onKeyDown={(e) => {
                        if (e.key === 'Escape') {
                          e.preventDefault()
                          clearSearch()
                        }
                      }}
                    />
                    <Icon
                      {...s}
                      icon="tabler:search"
                      width={14}
                      height={14}
                      className="absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
                    />
                    {searchText && (
                      <button
                        {...s}
                        type="button"
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        aria-label={t('清空搜索')}
                        onClick={clearSearch}
                      >
                        <Icon {...s} icon="tabler:x" width={14} height={14} />
                      </button>
                    )}
                  </div>
                </div>
              </div>
            </div>
            {groups.map(g => (
              <TabsContent {...s} key={g.name} value={g.name} className="pointer-events-auto">
                {activeHomeTool !== 'nodes' && (
                  <div {...s} className="mb-4 rounded-lg bg-background/50 px-3 py-2 text-sm text-muted-foreground">
                    {`${activeToolTitle} · 当前分组：${g.tab}（${groupNodeList.length} 台） `}
                  </div>
                )}
                <>
                  {activeHomeTool === 'topology'
                    ? <NodeTopologyPanel {...s} nodes={groupNodeList} />
                    : activeHomeTool === 'nodeCompare'
                      ? <NodeComparePanel {...s} nodes={groupNodeList} />
                      : activeHomeTool === 'providerValue'
                        ? <ProviderValuePanel {...s} nodes={groupNodeList} />
                        : activeHomeTool === 'healthSummary'
                          ? <HealthSummaryPanel {...s} nodes={groupNodeList} />
                          : activeHomeTool === 'snapshotExport'
                            ? <SnapshotExportPanel {...s} nodes={groupNodeList} />
                            : activeHomeTool === 'auditLog'
                              ? <AuditLogPanel {...s} />
                              : nodeList.length !== 0 && app.nodeViewMode === 'card'
                                ? (
                                    <TransitionGroup
                                      {...s}
                                      appear={enableNodeCardTransition}
                                      css={enableNodeCardTransition}
                                      name="node-card-switch"
                                      tag="div"
                                      className={nodeCardGridClass}
                                    >
                                      {nodeList.map((node, index) => (
                                        <NodeCardItem
                                          key={`${getNodeItemTransitionKey(node)}:${deferNodeCards ? 'deferred' : 'full'}`}
                                          style={getNodeItemTransitionStyle(index)}
                                        >
                                          <DeferredRender
                                            {...s}
                                            enabled={deferNodeCards}
                                            idleDelay={800 + index * 70}
                                            minHeight={deferredNodeCardHeight}
                                          >
                                            <NodeCard
                                              {...s}
                                              node={node}
                                              reduceMotion={reduceDenseNodeEffects}
                                              pingEnabled={isViewActive}
                                              onClick={() => handleNodeClick(node)}
                                              onPingClick={() => openPingDialog(node)}
                                            />
                                          </DeferredRender>
                                        </NodeCardItem>
                                      ))}
                                    </TransitionGroup>
                                  )
                                : nodeList.length !== 0 && app.nodeViewMode === 'list'
                                  ? (
                                      <NodeList
                                        {...s}
                                        nodes={nodeList}
                                        transitionKey={app.nodeSelectedGroup}
                                        sortResetKey={nodeListSortResetKey}
                                        pingEnabled={isViewActive}
                                        onClick={handleNodeClick}
                                        onPingClick={openPingDialog}
                                      />
                                    )
                                  : (
                                      <div {...s} className="text-muted-foreground text-center py-8">
                                        <Empty {...s} description={emptyDescription} />
                                      </div>
                                    )}
                </>
              </TabsContent>
            ))}
          </Tabs>
        </div>
      </div>
      {pingDialogNode && (
        <>
          <PingMonitorDialog
            {...s}
            open={Boolean(pingDialogNode)}
            uuid={pingDialogNode.uuid}
            nodeName={pingDialogNode.name}
            onOpenChange={(open: boolean) => {
              if (!open)
                setPingDialogNode(null)
            }}
          />
        </>
      )}
    </div>
  )
}
