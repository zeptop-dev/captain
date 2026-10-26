import { matchesPing } from '../glass-shared/ping-selection'
import { networkIdentity, networkLabel } from './lib/network'
import { nodeBandwidth } from './lib/node-bandwidth'
import { nodePrice } from './lib/node-price'
import { cpuSpec } from './lib/cpu-spec'
import { Badge, Box, Card, Container, Group, Progress, SimpleGrid, Stack, Text, Title, Tooltip, Select, TextInput } from '@mantine/core'
import type { UseQueryResult } from '@tanstack/react-query'
import { IconServer, IconArrowUp, IconArrowDown } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { publicShows, bytes, flag, pct, rate, uptime, type Node, type Snapshot, type PublicPingTask } from './lib/api'
import { Spark } from './components/Spark'
import { StorageSummary } from './components/StorageSummary'
import { NodeDetail } from './components/NodeDetail'

// The public status page: one card per node, live values, sparklines and
// the monthly traffic bar. Polls the snapshot at the beat interval.
export default function App({ snap }: { snap: UseQueryResult<Snapshot, Error> }) {
  const { t, i18n } = useTranslation()
  const [search, setSearch] = useState(''), [group, setGroup] = useState<string | null>(null), [status, setStatus] = useState<string | null>(null)
  const [open, setOpen] = useState<number | null>(null)
  useEffect(() => { if (snap.data?.title) document.title = snap.data.title }, [snap.data?.title])
  if (snap.isError) {
    const code = (snap.error as Error).message
    return <Container size="sm" py={80}><Card className="glass" bg="transparent" withBorder={false}><Title order={3}>{t(code === '401' ? 'page.signInRequired' : code === '403' ? 'page.staffOnly' : code === '404' ? 'page.off' : 'page.error')}</Title>{code === '401' && <Text mt="sm"><a href="/portal/login">{t('page.signIn')}</a></Text>}</Card></Container>
  }
  const s = snap.data
  if (!s) return null
  const show = (section: string) => publicShows(s, section)
  const filtered = s.nodes.filter((n) => n.name.toLowerCase().includes(search.toLowerCase()) && (!group || n.group === group) && (!status || n.online === (status === 'online')))
  const online = s.nodes.filter((n) => n.online).length
  const validNetworkNodes = s.nodes.filter((n) => n.online && n.host && n.host.valid?.network !== false)
  const totalUp = validNetworkNodes.reduce((a, n) => a + (n.host?.net_up ?? 0), 0)
  const totalDown = validNetworkNodes.reduce((a, n) => a + (n.host?.net_down ?? 0), 0)
  return (
    <Box pos="relative" style={{ zIndex: 1 }}>
      <div className="site-bg" />
      <Container size="lg" py="md">
        <Group justify="space-between">
          <Group gap="xs">{s.logo && <img src={s.logo} alt="" style={{ height: 28 }} />}<Text fw={800} fz="xl" className="gradient-text">{s.title}</Text></Group>
          <Group gap="md">
            <Badge variant="light" color="teal" size="lg" leftSection={<span className="live-dot" />}>{online} / {s.nodes.length} {t('page.online')}</Badge>
            {show('network') && <Text size="sm" c="dimmed"><IconArrowUp size={14} /> {validNetworkNodes.length ? rate(totalUp) : '—'} <IconArrowDown size={14} /> {validNetworkNodes.length ? rate(totalDown) : '—'}</Text>}
            <Select aria-label={t('page.language')} w={140} size="xs" value={i18n.resolvedLanguage} onChange={(v) => v && i18n.changeLanguage(v)} allowDeselect={false} data={[{ value: 'en', label: 'English' }, { value: 'zh-CN', label: '简体中文' }, { value: 'zh-TW', label: '繁體中文' }, { value: 'ja', label: '日本語' }, { value: 'ko', label: '한국어' }, { value: 'ru', label: 'Русский' }]} />
          </Group>
        </Group>
      </Container>
      <Container size="lg" pb={60}>
        <SimpleGrid cols={{ base: 1, sm: 3 }} mb="md"><TextInput aria-label={t('page.search')} placeholder={t('page.search')} value={search} onChange={(e) => setSearch(e.currentTarget.value)} /><Select aria-label={t('page.group')} placeholder={t('page.group')} clearable data={[...new Set(s.nodes.map((n) => n.group).filter((g): g is string => !!g))]} value={group} onChange={setGroup} /><Select aria-label={t('page.status')} placeholder={t('page.status')} clearable value={status} onChange={setStatus} data={['online', 'offline'].map((value) => ({ value, label: t(`page.${value}`) }))} /></SimpleGrid>
        <SimpleGrid cols={s.layout === 'compact' ? 1 : { base: 1, sm: 2, lg: 3 }} spacing="md">
          {filtered.map((n) => <NodeCard key={n.id} n={n} snapshot={s} onOpen={() => setOpen(n.id)} />)}
        </SimpleGrid>
        {filtered.length === 0 && <Text c="dimmed" ta="center" py={60}>{t('page.noNodes')}</Text>}
      </Container>
      <NodeDetail snapshot={s} node={s.nodes.find((n) => n.id === open) ?? null} onClose={() => setOpen(null)} />
    </Box>
  )
}

function NodeCard({ n, snapshot, onOpen }: { n: Node; snapshot: Snapshot; onOpen: () => void }) {
  const { t, i18n } = useTranslation()
  const show = (section: string) => publicShows(snapshot, section)
  const h = n.host
  const cpu = !h || h.valid?.cpu === false ? null : h.cpu_percent ?? 0
  const mem = !h || h.valid?.memory === false ? null : pct(h.mem_used, h.mem_total)
  const tr = n.traffic
  const trafficPct = tr.limit > 0 ? Math.min(100, Math.round((tr.used / tr.limit) * 100)) : 0
  const carriers: PublicPingTask[] = !show('latency') || !snapshot.carrier_ping ? [] : (n.ping_tasks ?? (h?.pings ?? []).map(p => ({ id: p.task_id, name: p.name, series: p.series, family: p.family, type: p.type }))).filter(p => p.id === 0)
  return (
    <Card className={`glass monitor-node ${snapshot.layout === 'compact' ? 'monitor-compact' : ''}`} withBorder={false} style={{ cursor: 'pointer', opacity: n.online ? 1 : 0.6 }} onClick={onOpen} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen() } }}>
      <Group justify="space-between" wrap="nowrap">
        <Group gap="xs" wrap="nowrap" style={{ minWidth: 0, flex: 1 }}>
          <Text fz="lg">{flag(n.info.region) || <IconServer size={18} />}</Text>
          <div style={{ minWidth: 0 }}>
            <Text fw={700} truncate>{n.name}</Text>
            <Text size="xs" c="dimmed" truncate>{[n.group, n.info.provider, h?.info?.os, h?.uptime ? uptime(h.uptime) : ''].filter(Boolean).join(' · ')}</Text>
          </div>
        </Group>
        <Badge size="sm" style={{ flexShrink: 0 }} color={n.online ? 'teal' : 'red'} variant={n.online ? 'light' : 'filled'}>{t(n.online ? 'page.online' : 'page.offline')}</Badge>
      </Group>
      {['cpu', 'memory'].some(show) && <SimpleGrid cols={['cpu', 'memory'].filter(show).length} spacing="xs" mt="sm">
        {show('cpu') && <Meter label="CPU" value={cpu} sub={show('system') ? cpuSpec(h?.info, i18n.language) : undefined} />}
        {show('memory') && <Meter label={t('page.memory')} value={mem} sub={mem == null ? undefined : bytes(h?.mem_total ?? 0)} />}
      </SimpleGrid>}
      {show('disk') && <StorageSummary node={n} />}
      {show('network') && <Group justify="space-between" mt="sm" gap="xs">
        {show('network') && <Text size="xs" c="dimmed"><IconArrowUp size={12} /> {!h || h.valid?.network === false ? '—' : rate(h.net_up ?? 0)} <IconArrowDown size={12} /> {!h || h.valid?.network === false ? '—' : rate(h.net_down ?? 0)}</Text>}
      </Group>}
      {show('history') && show('network') && snapshot.layout !== 'compact' && <Spark values={n.recent.flatMap((r, i) => [...(i > 0 && r.t - n.recent[i - 1].t > snapshot.beat_seconds * 2 ? [null] : []), r.valid?.network === false ? null : r.down])} color="#f59e0b" height={28} />}
      {carriers.length > 0 && (
        <Group gap="sm" mt="xs">
          {carriers.map(task => {
            const p = task.state === 'unsupported' ? undefined : h?.pings?.find(p => matchesPing(p, task))
            const label = networkLabel(task)
            const stale = p?.at && snapshot.now - p.at > Math.max(60, (p.quality?.interval_seconds ?? 30) * 2)
            const reason = task.reason || (stale ? 'stale' : p?.quality?.outcome)
            const unavailable = !p || stale || p.quality?.outcome === 'permission'
            const value = unavailable || !Number.isFinite(p.latency_ms) ? '—' : p.latency_ms < 0 ? '×' : `${Math.round(p.latency_ms)}ms`
            const loss = !unavailable && p.loss != null ? `${p.loss.toFixed(0)}%` : '—'
            return <Tooltip key={networkIdentity({ ...task, task_id: task.id })} label={`${label} ${t('page.loss')} ${loss}${reason ? ` · ${reason === 'stale' ? t('networkQuality.stale') : t(`networkQuality.outcomes.${reason}`)}` : ''}`}><Text size="xs" c={!p || stale ? 'dimmed' : p.latency_ms < 0 ? 'red' : p.latency_ms > 200 ? 'orange' : 'dimmed'}>{label} {value}</Text></Tooltip>
          })}
        </Group>
      )}
      {show('traffic') && (tr.limit > 0 ? (
        <Stack gap={2} mt="xs">
          <Group justify="space-between"><Text size="xs" c="dimmed">{t('page.traffic')}</Text><Text size="xs" c="dimmed">{bytes(tr.used)} / {bytes(tr.limit)}</Text></Group>
          <Progress value={trafficPct} size="sm" color={trafficPct > 90 ? 'red' : trafficPct > 70 ? 'orange' : 'teal'} />
        </Stack>
      ) : <Text size="xs" c="dimmed" mt="xs">{t('page.traffic')} {bytes(tr.used)}</Text>)}
      {nodeBandwidth(n.info.bandwidth, i18n.language) && <Text size="xs" c="dimmed" mt={4}>{t('page.bandwidth')}: {nodeBandwidth(n.info.bandwidth, i18n.language)}</Text>}
      {(n.info.expires_at || n.info.price) && <Text size="xs" c="dimmed" mt={4}>{[n.info.expires_at ? `${t('page.expires')} ${n.info.expires_at}` : '', nodePrice(n.info, i18n.language)].filter(Boolean).join(' · ')}</Text>}
    </Card>
  )
}

function Meter({ label, value, sub }: { label: string; value: number | null; sub?: string }) {
  return (
    <div>
      <Group justify="space-between" gap={4}><Text size="xs" c="dimmed">{label}</Text><Text size="xs" fw={600}>{value == null ? '—' : `${Math.round(value)}%`}</Text></Group>
      {value != null && <Progress value={value} size="xs" color={value > 90 ? 'red' : value > 70 ? 'orange' : undefined} />}
      {sub && <Text size="xs" c="dimmed" mt={2} truncate title={sub}>{sub}</Text>}
    </div>
  )
}
