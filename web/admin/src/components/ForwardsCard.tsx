import { Alert, Loader, ActionIcon, Badge, Button, Card, Code, Group, Modal, NumberInput, Select, Stack, Text, TextInput, Title, Tooltip, Box, Switch, SimpleGrid } from '@mantine/core'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowsSplit, IconLink, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Inbound, type Ingress, type Node } from '../lib/api'
import { useAuth } from '../lib/auth'
import { firstFreeIngressPort, ingressPort, forwardTargetAddress, forwardTargetProtocol } from '../lib/ingress'
import { ForwardHealth } from './ForwardHealth'
import type { ForwardHealthStatus } from '../lib/forward-health'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'

interface Hop { target: string; weight?: number }
interface Forward { ingress_id?: string; tag: string; listen?: string; port: number; protocol: string; target: string; inbound_id?: number; line_id?: number; line_member_id?: string; backend?: string; preserve_source?: boolean; proxy_protocol?: boolean; targets?: Hop[]; balance?: string; weight?: number }
interface HopStatus extends ForwardHealthStatus { target: string; up: boolean; rtt_ms: number; last_error?: string; active_conn: number; total_conn: number }
interface Status extends ForwardHealthStatus { up: boolean; rtt_ms: number; last_error: string; active_conn: number; total_conn: number; bytes_in: number; bytes_out: number; targets?: HopStatus[] }
interface Draft { targets: { target: string; weight: number }[]; balance: string; weight: number }

const hostPort = /^.+:\d+$/

// Port forwards on one node: raw TCP/UDP relays to a landing server. Pick a
// managed inbound as the target and an entry through this relay is one click.
export function ForwardsCard({ node, ingresses = [], embedded, readOnly = false }: { node: Node; ingresses?: Ingress[]; embedded?: boolean; readOnly?: boolean }) {
  // Embedded inside the node page's advanced section: no card frame, no title.
  const Root = embedded ? Box : Card
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['forwards', node.id], queryFn: () => api.get<{ forwards: Forward[]; status: Record<string, Status> }>(`/api/admin/nodes/${node.id}/forwards`), refetchInterval: 10000 })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const details = useQueries({ queries: (nodes.data ?? []).filter((n) => n.id !== node.id).map((n) => ({ queryKey: ['node', String(n.id)], queryFn: () => api.get<{ node: Node; inbounds: Inbound[]; ingresses?: Ingress[] }>(`/api/admin/nodes/${n.id}`), enabled: !readOnly })) })
  const targets = details.flatMap((d) => (d.data ? d.data.inbounds.map((ib) => ({ ib, node: d.data!.node, ingress: (d.data!.ingresses ?? []).find((g) => g.id === ib.IngressID) })) : []))
  // Unsaved edits overlay the loaded list; a save (or discard) clears them.
  const [edited, setEdited] = useState<Forward[] | null>(null)
  const list = edited ?? q.data?.forwards ?? []
  const dirty = edited !== null
  useSettingsDirty(!readOnly && dirty)
  const setList = (f: (cur: Forward[]) => Forward[]) => setEdited(f(list))
  const setDirty = (on: boolean) => { if (!on) setEdited(null) }
  // The server preserves current generated rules. Do not resubmit a stale copy
  // when an administrator changes a line while a legacy-rule draft is open.
  const save = useMutation({ mutationFn: (v: Forward[]) => api.put<{ forwards: Forward[] }>(`/api/admin/nodes/${node.id}/forwards`, { Forwards: v.filter(f => !f.line_id) }), onSuccess: r => { toast.ok(t('common.saved')); qc.setQueryData(['forwards', node.id], { ...q.data, forwards: r.forwards }); setDirty(false) }, onError: toast.err })
  const [ingressID, setIngressID] = useState('')
  const publicAddress = (f: Forward) => { const g = ingresses.find(g => String(g.id) === f.ingress_id); return { host: g ? g.entry_domain || g.entry_host : node.domain || node.public_addr, port: g ? ingressPort(g, f.port) : f.port } }

  const [port, setPort] = useState<number | string>('')
  const [proto, setProto] = useState('both')
  const [backend, setBackend] = useState('')
  const [preserve, setPreserve] = useState(false)
  const [proxyProto, setProxyProto] = useState(false)
  const [target, setTarget] = useState<string | null>(null)
  const [manual, setManual] = useState('')
  const add = () => {
    const p = Number(port)
    if (!p) return
    const pick = targets.find((x) => String(x.ib.ID) === target)
    // A line ingress is reached through its far-end address, never the node's public IP.
    const tgt = pick ? forwardTargetAddress(pick) : manual.trim()
    if (!tgt) return
    setDirty(true)
    setList((cur) => [...cur, { ingress_id: ingressID || undefined, tag: `fwd-${p}`, port: p, protocol: proto, target: tgt, inbound_id: pick?.ib.ID, backend: backend || undefined, preserve_source: backend === 'nft' && preserve ? true : undefined, proxy_protocol: backend !== 'nft' && proxyProto ? true : undefined }])
    setPort(''); setTarget(null); setManual('')
  }
  const mkEntry = useMutation({ mutationFn: (f: Forward) => {
    if (f.line_id) throw new Error(t('workspace.forbidden'))
    return api.post('/api/admin/entries', { Name: `${node.name} → ${targets.find((x) => x.ib.ID === f.inbound_id)?.node.name ?? f.target}`, InboundID: f.inbound_id, DisplayHost: publicAddress(f).host, DisplayPort: publicAddress(f).port, Rate: 1, Enabled: true })
  }, onSuccess: () => { toast.ok(t('forwards.entryMade')); qc.invalidateQueries({ queryKey: ['entries'] }) }, onError: toast.err })
  // Further targets of one rule are edited in a dialog and land in the
  // list like any other change, saved with the card's Save button.
  const [editIdx, setEditIdx] = useState<number | null>(null)
  const [draft, setDraft] = useState<Draft>({ targets: [], balance: 'failover', weight: 1 })
  const editing = editIdx === null ? null : list[editIdx]
  const roundRobin = draft.balance === 'roundrobin' || editing?.backend === 'realm'
  const openTargets = (i: number) => {
    const f = list[i]
    if (f.line_id) return
    setDraft({ targets: (f.targets ?? []).map((x) => ({ target: x.target, weight: x.weight || 1 })), balance: f.balance || (f.backend === 'realm' ? 'roundrobin' : 'failover'), weight: f.weight || 1 })
    setEditIdx(i)
  }
  const applyTargets = () => {
    if (editIdx === null || list[editIdx]?.line_id) return
    const hops = draft.targets.filter((x) => x.target.trim() !== '').map((x) => ({ target: x.target.trim(), ...(roundRobin && x.weight !== 1 ? { weight: x.weight } : {}) }))
    setDirty(true)
    setList((cur) => cur.map((f, j) => {
      if (j !== editIdx) return f
      const { targets: _t, balance: _b, weight: _w, ...rest } = f
      if (hops.length === 0) return rest
      return { ...rest, targets: hops, balance: roundRobin ? 'roundrobin' : 'failover', ...(roundRobin && draft.weight !== 1 ? { weight: draft.weight } : {}) }
    }))
    setEditIdx(null)
  }
  const draftValid = draft.targets.every((x) => x.target.trim() === '' || hostPort.test(x.target.trim()))
  const describe = (f: Forward) => { const x = targets.find((y) => y.ib.ID === f.inbound_id); return x ? `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol})` : '' }
  const owner = (f: Forward) => f.line_id ? me?.role === 'admin'
    ? <Button component={Link} variant="subtle" size="compact-xs" to={`/entries?tab=lines&section=forward&line=${f.line_id}#line-${f.line_id}`}>{t('lines.tab')} #{f.line_id}</Button>
    : <Badge variant="light">{t('lines.tab')} #{f.line_id}</Badge> : null
  if (q.isPending) return <Loader size="sm" />
  if (q.isError) return <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>
  if (readOnly) return <Stack gap="xs">
    {list.map(f => <Stack key={f.tag} gap={4}>
      <Group><Code>{f.protocol === 'both' ? 'tcp+udp' : f.protocol} :{f.port} → {f.target}</Code><Badge variant="light">{f.backend || t('forwards.backendRelay')}</Badge><ForwardHealth status={q.data?.status?.[f.tag]} protocol={f.protocol} />{owner(f)}</Group>
      {!!f.targets?.length && <Text size="xs" c="dimmed">{t(f.balance === 'roundrobin' || f.backend === 'realm' ? 'forwards.balanceRoundRobin' : 'forwards.balanceFailover')}: {[f.target, ...f.targets.map(target => target.target)].join(', ')}</Text>}
    </Stack>)}
    {list.length === 0 && <Text size="sm" c="dimmed">{t('common.empty')}</Text>}
  </Stack>
  return (
    <Root mb={embedded ? 0 : "lg"}>
      {!embedded && <Title order={5} mb={4}>{t('forwards.title')}</Title>}
      <Text size="xs" c="dimmed" mb="sm">{t('forwards.hint')} {t('forwards.nftHint')}</Text>
      <Stack gap="xs">
        {list.map((f, i) => {
          const s = q.data?.status?.[f.tag]
          return (
            <Group key={i} justify="space-between" align="flex-start">
              <Group gap="xs" style={{ minWidth: 0 }}>
                <Select aria-label={t('inbounds.ingress')} w={180} value={f.ingress_id || ''} disabled={!!f.line_id} allowDeselect={false} data={[{ value: '', label: t('inbounds.ingressDirect'), disabled: ingresses.some(g => g.require_ingress) }, ...ingresses.map(g => ({ value: String(g.id), label: g.name }))]} onChange={v => { if (f.line_id) return; setDirty(true); setList(cur => cur.map((x, j) => j === i ? { ...x, ingress_id: v || undefined, listen: '' } : x)) }} />
                <Code>{f.protocol === 'both' ? 'tcp+udp' : f.protocol} :{f.port}</Code><Text size="sm">→</Text><Code>{f.target}</Code>{(f.targets ?? []).length > 0 && <Tooltip label={(f.targets ?? []).map((x) => x.target).join(', ')}><Badge size="xs" variant="light">+{f.targets!.length} · {f.balance === 'roundrobin' ? t('forwards.roundRobinShort') : t('forwards.failoverShort')}</Badge></Tooltip>}{f.proxy_protocol && <Badge size="xs" variant="outline" color="teal">PROXY</Badge>}{f.backend === 'realm' && <Badge size="xs" variant="outline" color="indigo">realm</Badge>}{f.backend === 'nft' && <Badge size="xs" variant="outline" color="grape">nft{f.preserve_source ? ' · src' : ''}</Badge>}
                {describe(f) && <Text size="xs" c="dimmed" truncate>{describe(f)}</Text>}
                {s?.targets ? s.targets.map(h => <Box key={h.target} title={h.target}><ForwardHealth status={h} protocol={f.protocol} /></Box>) : s && <ForwardHealth status={s} protocol={f.protocol} />}
              </Group>
              {f.line_id ? owner(f) : <Group gap={4} wrap="nowrap">
                {f.backend !== 'nft' && <Tooltip label={t('forwards.editTargets')}><ActionIcon variant="subtle" color="gray" onClick={() => openTargets(i)}><IconArrowsSplit size={16} /></ActionIcon></Tooltip>}
                {f.inbound_id && !dirty && publicAddress(f).host && publicAddress(f).port > 0 && <Tooltip label={t('forwards.makeEntry')}><ActionIcon variant="subtle" onClick={() => mkEntry.mutate(f)}><IconLink size={16} /></ActionIcon></Tooltip>}
                <ActionIcon variant="subtle" color="red" onClick={() => { setDirty(true); setList(cur => cur.filter((_, j) => j !== i)) }}><IconTrash size={16} /></ActionIcon>
              </Group>}
            </Group>
          )
        })}
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          <Select label={t('inbounds.ingress')} allowDeselect={false} value={ingressID} data={[{ value: '', label: t('inbounds.ingressDirect'), disabled: ingresses.some(g => g.require_ingress) }, ...ingresses.map(g => ({ value: String(g.id), label: g.name }))]} onChange={v => { setIngressID(v || ''); const g = ingresses.find(g => String(g.id) === v); if (g) setPort(firstFreeIngressPort(g, list.map(f => f.port)) || '') }} />
          <NumberInput label={t('forwards.port')} min={1} max={65535} value={port} onChange={setPort} />
          <Select label={t('forwards.protocol')} data={[{ value: 'both', label: 'tcp+udp' }, { value: 'tcp', label: 'tcp' }, { value: 'udp', label: 'udp' }]} value={proto} onChange={(v) => setProto(v ?? 'both')} allowDeselect={false} />
          <Select label={t('forwards.target')} style={{ flex: 2 }} searchable clearable placeholder={t('forwards.pickInbound')} data={targets.map((x) => ({ value: String(x.ib.ID), label: `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol}:${x.ib.Port})${x.ingress ? ` · ${x.ingress.name} ${x.ingress.line_ip || ''}` : ''}` }))} value={target} onChange={v => { setTarget(v); const pick = targets.find(x => String(x.ib.ID) === v); if (pick) { const protocol = forwardTargetProtocol(pick.ib.Protocol); setProto(protocol); if (protocol === 'udp') setProxyProto(false) } }} />
          {!target && <TextInput label={t('forwards.manual')} placeholder="198.51.100.20:443" style={{ flex: 2 }} value={manual} onChange={(e) => setManual(e.currentTarget.value)} />}
          <Select label={t('forwards.backend')} data={[{ value: '', label: t('forwards.backendRelay') }, { value: 'nft', label: t('forwards.backendNft') }, { value: 'realm', label: t('forwards.backendRealm') }]} value={backend} onChange={(v) => setBackend(v ?? '')} allowDeselect={false} />
          {backend === 'realm' && <Text size="xs" c="dimmed" maw={320} mb={6}>{t('forwards.backendRealmHint')}</Text>}
          {backend === 'nft' && <Switch label={t('forwards.preserve')} mb={6} checked={preserve} onChange={(e) => setPreserve(e.currentTarget.checked)} />}
          {backend !== 'nft' && <Switch label={t('forwards.proxyProtocol')} mb={6} checked={proxyProto} onChange={(e) => setProxyProto(e.currentTarget.checked)} />}
          <Button variant="light" leftSection={<IconPlus size={14} />} onClick={add} disabled={!port || (!target && !manual.trim()) || (!ingressID && ingresses.some(g => g.require_ingress)) || q.isError || !q.data}>{t('forwards.add')}</Button>
        </SimpleGrid>
        {dirty && <Group justify="flex-end"><Button size="xs" loading={save.isPending} onClick={() => save.mutate(list)}>{t('common.save')}</Button></Group>}
      </Stack>
      <Modal opened={editing !== null} onClose={() => setEditIdx(null)} title={t('forwards.editTargets')} size="lg">
        {editing && <Stack gap="sm">
          <Group gap="xs"><Text size="sm" c="dimmed">{t('forwards.firstTarget')}</Text><Code>{editing.target}</Code></Group>
          <Text size="xs" c="dimmed">{t('forwards.moreTargetsHint')}</Text>
          {draft.targets.map((x, j) => (
            <Group key={j} gap="xs" wrap="nowrap" align="flex-start">
              <TextInput style={{ flex: 1 }} placeholder="198.51.100.20:443" value={x.target} error={x.target.trim() !== '' && !hostPort.test(x.target.trim())}
                onChange={(e) => { const v = e.currentTarget.value; setDraft((d) => ({ ...d, targets: d.targets.map((y, k) => (k === j ? { ...y, target: v } : y)) })) }} />
              {roundRobin && <NumberInput w={90} min={1} max={100} placeholder={t('forwards.weight')} value={x.weight}
                onChange={(v) => setDraft((d) => ({ ...d, targets: d.targets.map((y, k) => (k === j ? { ...y, weight: Number(v) || 1 } : y)) }))} />}
              <ActionIcon variant="subtle" color="red" mt={6} onClick={() => setDraft((d) => ({ ...d, targets: d.targets.filter((_, k) => k !== j) }))}><IconTrash size={16} /></ActionIcon>
            </Group>
          ))}
          <Group gap="xs" wrap="nowrap">
            <Select style={{ flex: 1 }} searchable clearable placeholder={t('forwards.pickInbound')} value={null}
              data={targets.map((x) => ({ value: String(x.ib.ID), label: `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol}:${x.ib.Port})${x.ingress ? ` · ${x.ingress.name} ${x.ingress.line_ip || ''}` : ''}` }))}
              onChange={(v) => { const pick = targets.find((x) => String(x.ib.ID) === v); if (pick) setDraft((d) => ({ ...d, targets: [...d.targets, { target: forwardTargetAddress(pick), weight: 1 }] })) }} />
            <Button variant="light" leftSection={<IconPlus size={14} />} onClick={() => setDraft((d) => ({ ...d, targets: [...d.targets, { target: '', weight: 1 }] }))}>{t('forwards.addTarget')}</Button>
          </Group>
          {draft.targets.length > 0 && (editing.backend === 'realm'
            ? <Text size="xs" c="dimmed">{t('forwards.realmRoundRobinOnly')}</Text>
            : <Select label={t('forwards.balance')} description={draft.balance === 'roundrobin' ? t('forwards.balanceRoundRobinHint') : t('forwards.balanceFailoverHint')} allowDeselect={false}
                data={[{ value: 'failover', label: t('forwards.balanceFailover') }, { value: 'roundrobin', label: t('forwards.balanceRoundRobin') }]}
                value={draft.balance} onChange={(v) => setDraft((d) => ({ ...d, balance: v ?? 'failover' }))} />)}
          {draft.targets.length > 0 && roundRobin && <NumberInput label={t('forwards.primaryWeight')} min={1} max={100} w={200} value={draft.weight} onChange={(v) => setDraft((d) => ({ ...d, weight: Number(v) || 1 }))} />}
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditIdx(null)}>{t('common.cancel')}</Button><Button onClick={applyTargets} disabled={!draftValid}>{t('common.confirm')}</Button></Group>
        </Stack>}
      </Modal>
    </Root>
  )
}
