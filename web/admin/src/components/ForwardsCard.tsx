import { ActionIcon, Badge, Button, Card, Code, Group, NumberInput, Select, Stack, Text, TextInput, Title, Tooltip } from '@mantine/core'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconLink, IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Inbound, type Node } from '../lib/api'
import { bytes } from '../lib/format'
import { toast } from '../lib/notify'

interface Forward { tag: string; listen?: string; port: number; protocol: string; target: string; inbound_id?: number }
interface Status { up: boolean; rtt_ms: number; last_error: string; active_conn: number; total_conn: number; bytes_in: number; bytes_out: number }

// Port forwards on one node: raw TCP/UDP relays to a landing server. Pick a
// managed inbound as the target and an entry through this relay is one click.
export function ForwardsCard({ node }: { node: Node }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['forwards', node.id], queryFn: () => api.get<{ forwards: Forward[]; status: Record<string, Status> }>(`/api/admin/nodes/${node.id}/forwards`), refetchInterval: 10000 })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const details = useQueries({ queries: (nodes.data ?? []).filter((n) => n.id !== node.id).map((n) => ({ queryKey: ['node', String(n.id)], queryFn: () => api.get<{ node: Node; inbounds: Inbound[] }>(`/api/admin/nodes/${n.id}`) })) })
  const targets = details.flatMap((d) => (d.data ? d.data.inbounds.map((ib) => ({ ib, node: d.data!.node })) : []))
  const [list, setList] = useState<Forward[]>([])
  useEffect(() => { if (q.data) setList(q.data.forwards ?? []) }, [q.data])
  const save = useMutation({ mutationFn: (v: Forward[]) => api.put(`/api/admin/nodes/${node.id}/forwards`, { Forwards: v }), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['forwards', node.id] }) }, onError: toast.err })
  const [port, setPort] = useState<number | string>('')
  const [proto, setProto] = useState('both')
  const [target, setTarget] = useState<string | null>(null)
  const [manual, setManual] = useState('')
  const add = () => {
    const p = Number(port)
    if (!p) return
    const pick = targets.find((x) => String(x.ib.ID) === target)
    const tgt = pick ? `${pick.node.public_addr}:${pick.ib.Port}` : manual.trim()
    if (!tgt) return
    setList((cur) => [...cur, { tag: `fwd-${p}`, port: p, protocol: proto, target: tgt, inbound_id: pick?.ib.ID }])
    setPort(''); setTarget(null); setManual('')
  }
  const mkEntry = useMutation({ mutationFn: (f: Forward) => api.post('/api/admin/entries', { Name: `${node.name} → ${targets.find((x) => x.ib.ID === f.inbound_id)?.node.name ?? f.target}`, InboundID: f.inbound_id, DisplayHost: node.public_addr, DisplayPort: f.port, Rate: 1, Enabled: true }), onSuccess: () => { toast.ok(t('forwards.entryMade')); qc.invalidateQueries({ queryKey: ['entries'] }) }, onError: toast.err })
  const dirty = JSON.stringify(list) !== JSON.stringify(q.data?.forwards ?? [])
  const describe = (f: Forward) => { const x = targets.find((y) => y.ib.ID === f.inbound_id); return x ? `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol})` : '' }
  return (
    <Card mb="lg">
      <Title order={5} mb={4}>{t('forwards.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('forwards.hint')}</Text>
      <Stack gap="xs">
        {list.map((f, i) => {
          const s = q.data?.status?.[f.tag]
          return (
            <Group key={i} justify="space-between" wrap="nowrap">
              <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
                <Code>{f.protocol === 'both' ? 'tcp+udp' : f.protocol} :{f.port}</Code><Text size="sm">→</Text><Code>{f.target}</Code>
                {describe(f) && <Text size="xs" c="dimmed" truncate>{describe(f)}</Text>}
                {s && <Tooltip label={s.last_error || `${s.active_conn} / ${s.total_conn} conn · ${bytes(s.bytes_in)} in · ${bytes(s.bytes_out)} out`}><Badge size="xs" color={s.up ? 'teal' : 'red'} variant="light">{s.up ? `${s.rtt_ms} ms` : t('forwards.down')}</Badge></Tooltip>}
              </Group>
              <Group gap={4} wrap="nowrap">
                {f.inbound_id && !dirty && <Tooltip label={t('forwards.makeEntry')}><ActionIcon variant="subtle" onClick={() => mkEntry.mutate(f)}><IconLink size={16} /></ActionIcon></Tooltip>}
                <ActionIcon variant="subtle" color="red" onClick={() => setList((cur) => cur.filter((_, j) => j !== i))}><IconTrash size={16} /></ActionIcon>
              </Group>
            </Group>
          )
        })}
        <Group align="flex-end" wrap="nowrap">
          <NumberInput label={t('forwards.port')} w={110} min={1} max={65535} value={port} onChange={setPort} />
          <Select label={t('forwards.protocol')} w={110} data={[{ value: 'both', label: 'tcp+udp' }, { value: 'tcp', label: 'tcp' }, { value: 'udp', label: 'udp' }]} value={proto} onChange={(v) => setProto(v ?? 'both')} allowDeselect={false} />
          <Select label={t('forwards.target')} style={{ flex: 2 }} searchable clearable placeholder={t('forwards.pickInbound')} data={targets.map((x) => ({ value: String(x.ib.ID), label: `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol}:${x.ib.Port})` }))} value={target} onChange={setTarget} />
          {!target && <TextInput label={t('forwards.manual')} placeholder="1.2.3.4:443" style={{ flex: 2 }} value={manual} onChange={(e) => setManual(e.currentTarget.value)} />}
          <Button variant="light" leftSection={<IconPlus size={14} />} onClick={add} disabled={!port || (!target && !manual.trim())}>{t('forwards.add')}</Button>
        </Group>
        {dirty && <Group justify="flex-end"><Button size="xs" loading={save.isPending} onClick={() => save.mutate(list)}>{t('common.save')}</Button></Group>}
      </Stack>
    </Card>
  )
}
