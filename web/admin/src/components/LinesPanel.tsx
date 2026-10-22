import { ActionIcon, Alert, Badge, Box, Button, Card, Code, Group, Loader, Modal, NumberInput, Select, SimpleGrid, Stack, Text, TextInput, Title, Tooltip } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ForwardAddressMode, type Line, type LineGroup, type LineMember, type Node } from '../lib/api'
import { freshLineStatus, lineSupportsGroups, proxyServer, type EntryNodeDetail, type InboundChoice } from '../lib/entry-flow'
import { firstFreeIngressPort, forwardTargetAddress, forwardingAddress, hostPort, ingressPort } from '../lib/ingress'
import { useSettingsDirty } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { ForwardAddressModeSelect } from './ForwardAddressModeSelect'
import { ForwardConnectivityCheck } from './ForwardConnectivityCheck'
import { EntryInboundCreator } from './EntryInboundCreator'

interface DraftMember extends LineMember { key: string }
interface DraftGroup extends Omit<LineGroup, 'members'> { key: string; members: DraftMember[] }
interface Draft { name: string; backend: Line['backend']; protocol: Line['protocol']; target_inbound_id: number; target_address_mode: ForwardAddressMode; groups: DraftGroup[] }
const newMember = (): DraftMember => ({ key: crypto.randomUUID(), id: '', node_id: 0, port: 0, weight: 1, address_mode: 'default' })
const newGroup = (): DraftGroup => ({ key: crypto.randomUUID(), strategy: 'failover', members: [newMember()] })
const move = <T,>(items: T[], from: number, to: number) => { const copy = [...items]; const [item] = copy.splice(from, 1); copy.splice(to, 0, item); return copy }

export function LinesPanel({ lines, nodes, inbounds, loading, error, observedAt, focusID, onRetry, onEdit, onCreateNode }: {
  lines: Line[]; nodes: Node[]; inbounds: InboundChoice[]; loading: boolean; error: boolean; observedAt: number; focusID?: number; onRetry: () => void; onEdit: (line: Line) => void; onCreateNode: (line: Line) => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const focusedID = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (!focusID) { focusedID.current = undefined; return }
    if (loading || focusedID.current === focusID) return
    const card = document.getElementById(`line-${focusID}`)
    if (card) { card.scrollIntoView({ block: 'center' }); focusedID.current = focusID }
  }, [focusID, loading, lines])
  const del = useMutation({ mutationFn: (line: Line) => api.del(`/api/admin/lines/${line.id}?version=${line.version}`), onSuccess: () => { qc.invalidateQueries({ queryKey: ['lines'] }); qc.invalidateQueries({ queryKey: ['entries'] }); toast.ok(t('common.deleted')) } })
  return <Stack>
    <Alert color="blue">{t('lines.scopeHint')}</Alert>
    {loading && <Loader size="sm" />}
    {error && <Alert color="red"><Text size="sm">{t('lines.loadError')}</Text><Button size="xs" variant="subtle" onClick={onRetry}>{t('entryFlow.retry')}</Button></Alert>}
    {del.isError && <Alert color="red">{del.error.message}</Alert>}
    {!loading && !error && lines.length === 0 && <Card><Text c="dimmed" ta="center" py="lg">{t('lines.empty')}</Text></Card>}
    {lines.map(line => {
      const target = inbounds.find(c => c.ib.ID === line.target_inbound_id)
      return <Card key={line.id} id={`line-${line.id}`} withBorder style={{ scrollMarginTop: 90, borderColor: focusID === line.id ? 'var(--mantine-color-indigo-5)' : undefined }}>
        <Group justify="space-between" align="flex-start" mb="sm">
          <Stack gap={4}><Title order={4}>{line.name}</Title><Group gap="xs"><Badge variant="light">{t(`lines.backends.${line.backend}`)}</Badge><Badge variant="outline" color="gray">{line.protocol === 'both' ? 'TCP + UDP' : line.protocol.toUpperCase()}</Badge></Group></Stack>
          <Group gap="xs"><Button size="xs" variant="light" disabled={!line.endpoints?.some(e => e.host.trim()) || error} onClick={() => onCreateNode(line)}>{t('entryFlow.createNode')}</Button><ActionIcon variant="subtle" aria-label={t('common.edit')} onClick={() => onEdit(line)} disabled={del.isPending}><IconPencil size={17} /></ActionIcon><ActionIcon color="red" variant="subtle" aria-label={t('common.delete')} disabled={del.isPending} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('lines.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(line) })}><IconTrash size={17} /></ActionIcon></Group>
        </Group>
        <Stack gap="xs">
          {!line.endpoints?.some(e => e.host.trim()) && <Alert color="yellow">{t('entryFlow.lineEndpointMissing')}</Alert>}
          {line.groups.map((group, i) => <Box key={i} pl="sm" style={{ borderLeft: '2px solid var(--mantine-color-indigo-light)' }}>
            <Text size="xs" fw={600} mb={4}>{i === 0 ? t('lines.entryGroup') : t('lines.hopGroup', { number: i + 1 })}{i > 0 && ` · ${t(`lines.strategies.${group.strategy}`)}`}</Text>
            <Group gap="sm" align="flex-start">{group.members.map(member => {
              const status = line.status?.find(s => s.member_id === member.id)
              const fresh = status && freshLineStatus(status.updated_at, observedAt)
              const state = fresh ? status.health : 'unknown'
              const transport = fresh ? status.probe_protocol : 'none'
              return <Stack key={member.id} gap={3}>
                <Text size="sm">{nodes.find(n => n.id === member.node_id)?.name ?? `#${member.node_id}`} <Code>{member.port}</Code>{i > 0 && member.address_mode === 'internal' && <Badge ml={4} size="xs" variant="light">{t('nodes.internalAddr')}</Badge>}{i > 0 && group.strategy === 'roundrobin' && <Text component="span" size="xs" c="dimmed"> · {t('lines.weightValue', { weight: member.weight })}</Text>}</Text>
                <Group gap={4}><Tooltip label={t('lines.probeHint')}><Badge size="xs" color={state === 'up' && transport === 'tcp' ? 'teal' : state === 'down' ? 'red' : 'gray'} variant="light">{transport === 'tcp' && state !== 'unknown' ? `TCP · ${state === 'up' ? `${status?.rtt_ms} ms` : t('lines.probeDown')}` : transport === 'backend' && state === 'down' ? t('lines.backendFailed') : t('lines.probeUnknown')}</Badge></Tooltip>{line.protocol !== 'tcp' && <Badge size="xs" color="gray" variant="outline">{t('lines.udpUntested')}</Badge>}</Group>
              </Stack>
            })}</Group>
          </Box>)}
          <Text size="sm" mt="xs">{t('lines.target')}: {target ? `${target.node.name} / ${target.ib.Tag}` : `#${line.target_inbound_id}`} {line.target_address_mode === 'internal' && <Badge size="xs" variant="light">{t('nodes.internalAddr')}</Badge>}</Text>
          {target && line.target_address_mode === 'internal' && <Code>{forwardTargetAddress(target, 'internal') || t('forwardAddress.internalMissing')}</Code>}
          <Text size="xs" c="dimmed">{t('lines.probeHint')}</Text>
          {(line.endpoints?.length ?? 0) > 0 && <Group gap="xs">{line.endpoints.filter(endpoint => endpoint.host.trim()).map(endpoint => <Code key={endpoint.member_id}>{hostPort(endpoint.host, endpoint.port)}</Code>)}</Group>}
        </Stack>
      </Card>
    })}
  </Stack>
}

export function LineEditor({ line, nodes, details, inbounds, loading, loadError, onReload, onClose }: {
  line?: Line; nodes: Node[]; details: EntryNodeDetail[]; inbounds: InboundChoice[]; loading: boolean; loadError: boolean; onReload: () => void; onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [initial] = useState<Draft>(() => line ? { name: line.name, backend: line.backend, protocol: line.protocol, target_inbound_id: line.target_inbound_id, target_address_mode: line.target_address_mode || 'default', groups: line.groups.map(group => ({ ...group, key: crypto.randomUUID(), members: group.members.map(member => ({ ...member, key: crypto.randomUUID() })) })) } : { name: '', backend: 'relay', protocol: 'both', target_inbound_id: 0, target_address_mode: 'default', groups: [newGroup()] })
  const [draft, setDraft] = useState(initial)
  const [creatingInbound, setCreatingInbound] = useState(false)
  const [createdInbound, setCreatedInbound] = useState<InboundChoice | null>(null)
  const choices = createdInbound && !inbounds.some(c => c.ib.ID === createdInbound.ib.ID) ? [...inbounds, createdInbound] : inbounds
  const target = choices.find(c => c.ib.ID === draft.target_inbound_id)
  const options = useQuery({ queryKey: ['line-options', draft.target_inbound_id], queryFn: () => api.get<{ protocol: Line['protocol'] | ''; error?: string }>(`/api/admin/lines/options?inbound_id=${draft.target_inbound_id}`), enabled: !!draft.target_inbound_id })
  const protocol = options.data?.protocol || draft.protocol
  const members = draft.groups.flatMap(g => g.members)
  const duplicated = new Set(members.map(m => m.node_id)).size !== members.length
  const mixedFailover = protocol !== 'tcp' && draft.groups.some((g, i) => i > 0 && g.members.length > 1 && g.strategy !== 'roundrobin')
  const internalMissing = draft.target_address_mode === 'internal' && !target?.node.internal_addr?.trim() || draft.groups.some((g, i) => i > 0 && g.members.some(m => m.address_mode === 'internal' && !nodes.find(n => n.id === m.node_id)?.internal_addr?.trim()))
  const valid = !internalMissing && draft.name.trim() && target?.ib.Enabled && options.data?.protocol && !options.isError && !options.data.error && !mixedFailover && members.every(m => m.node_id && Number.isInteger(m.port) && m.port > 0 && m.port < 65536 && m.weight >= 1 && m.weight <= 100) && !duplicated && !members.some(m => m.node_id === target.ib.NodeID) && lineSupportsGroups(draft.backend, draft.groups)
  const dirty = JSON.stringify(draft) !== JSON.stringify(initial) || !!createdInbound
  const save = useMutation({
    mutationFn: () => {
      const body = { name: draft.name.trim(), backend: draft.backend, protocol, target_inbound_id: draft.target_inbound_id, target_address_mode: draft.target_address_mode, groups: draft.groups.map((g, i) => ({ strategy: g.strategy, members: g.members.map(m => ({ id: m.id, node_id: m.node_id, port: m.port, ingress_id: m.ingress_id || null, weight: m.weight, address_mode: i > 0 ? m.address_mode || 'default' : 'default' })) })), ...(line ? { version: line.version } : {}) }
      return line ? api.put<Line>(`/api/admin/lines/${line.id}`, body) : api.post<Line>('/api/admin/lines', body)
    },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['lines'] }); qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['node'] }); toast.ok(t('common.saved')); onClose() },
  })
  useSettingsDirty(dirty || save.isPending)
  const close = () => {
    if (save.isPending) return
    if (!dirty) { onClose(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{createdInbound ? t('entryFlow.createdResourcesRemain') : t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose })
  }
  const patchGroup = (key: string, update: Partial<DraftGroup>) => setDraft(d => ({ ...d, groups: d.groups.map(g => g.key === key ? { ...g, ...update } : g) }))
  const patchMember = (group: DraftGroup, key: string, update: Partial<DraftMember>) => patchGroup(group.key, { members: group.members.map(m => m.key === key ? { ...m, ...update } : m) })
  return <>
    <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={line ? t('lines.edit') : t('lines.create')} size="xl">
      <form onSubmit={event => { event.preventDefault(); if (valid) save.mutate() }}>
        <Stack>
          {loadError && <Alert color="yellow"><Text size="sm">{t('entryFlow.partialLoad')}</Text><Button variant="subtle" size="xs" onClick={onReload}>{t('entryFlow.retry')}</Button></Alert>}
          {save.isError && <Alert color="red"><Text size="sm">{save.error.message}</Text><Text size="xs" mt={4}>{t('lines.saveErrorHint')}</Text></Alert>}
          <Box component="fieldset" disabled={save.isPending} style={{ margin: 0, padding: 0, border: 0, minWidth: 0 }}><Stack>
            <TextInput label={t('lines.name')} required value={draft.name} onChange={event => { const name = event.currentTarget.value; setDraft(d => ({ ...d, name })) }} />
            <SimpleGrid cols={{ base: 1, sm: 2 }}>
              <Select label={t('lines.backend')} allowDeselect={false} value={draft.backend} onChange={value => setDraft(d => ({ ...d, backend: value as Line['backend'] }))} data={(['relay', 'nft', 'realm'] as const).map(value => ({ value, label: t(`lines.backends.${value}`), disabled: !lineSupportsGroups(value, draft.groups) }))} />
              <TextInput label={t('lines.protocol')} readOnly value={options.isFetching ? t('entryFlow.loading') : options.data?.protocol ? protocol === 'both' ? 'TCP + UDP' : protocol.toUpperCase() : '—'} />
            </SimpleGrid>
            <Alert color="blue">{t(`lines.backendHints.${draft.backend}`)}</Alert>
            <Text size="sm" c="dimmed">{t('lines.groupHint')}</Text>
            {draft.groups.map((group, index) => <Card key={group.key} withBorder padding="sm">
              <Group justify="space-between" mb="sm"><Text fw={600}>{index === 0 ? t('lines.entryGroup') : t('lines.hopGroup', { number: index + 1 })}</Text><Group gap={3}>
                <ActionIcon variant="subtle" aria-label={t('lines.moveUp')} disabled={index === 0 || !lineSupportsGroups(draft.backend, move(draft.groups, index, index - 1))} onClick={() => setDraft(d => ({ ...d, groups: move(d.groups, index, index - 1) }))}><IconArrowUp size={16} /></ActionIcon>
                <ActionIcon variant="subtle" aria-label={t('lines.moveDown')} disabled={index === draft.groups.length - 1 || !lineSupportsGroups(draft.backend, move(draft.groups, index, index + 1))} onClick={() => setDraft(d => ({ ...d, groups: move(d.groups, index, index + 1) }))}><IconArrowDown size={16} /></ActionIcon>
                <ActionIcon variant="subtle" color="red" aria-label={t('lines.removeGroup')} disabled={draft.groups.length === 1} onClick={() => setDraft(d => ({ ...d, groups: d.groups.filter(g => g.key !== group.key) }))}><IconTrash size={16} /></ActionIcon>
              </Group></Group>
              {index === 0 ? <Text size="xs" c="dimmed" mb="sm">{t('lines.entryHint')}</Text> : <Select label={t('lines.strategy')} description={group.strategy === 'failover' ? t('lines.failoverHint') : t('lines.roundrobinHint')} allowDeselect={false} mb="sm" data={[{ value: 'failover', label: t('lines.strategies.failover'), disabled: protocol !== 'tcp' && group.members.length > 1 }, { value: 'roundrobin', label: t('lines.strategies.roundrobin') }]} value={group.strategy} disabled={draft.backend !== 'relay'} onChange={value => patchGroup(group.key, { strategy: value as LineGroup['strategy'] })} />}
              <Stack gap="sm">{group.members.map((member, memberIndex) => {
                const detail = details.find(d => d.node.id === member.node_id)
                const ingresses = detail?.ingresses ?? []
                const ingress = ingresses.find(g => g.id === member.ingress_id)
                const server = detail?.node ?? nodes.find(n => n.id === member.node_id)
                const address = server ? forwardingAddress(server, member.port, ingress, index > 0 ? member.address_mode : 'default') : ''
                return <Box key={member.key} style={{ borderTop: memberIndex ? '1px solid var(--mantine-color-default-border)' : undefined, paddingTop: memberIndex ? 12 : undefined }}>
                  <SimpleGrid cols={{ base: 1, sm: 2 }}>
                    <Select label={t('entryFlow.server')} searchable required allowDeselect={false} value={member.node_id ? String(member.node_id) : null} data={nodes.filter(proxyServer).map(n => ({ value: String(n.id), label: n.name, disabled: n.id === target?.ib.NodeID || members.some(m => m.key !== member.key && m.node_id === n.id) }))} onChange={value => patchMember(group, member.key, { node_id: Number(value), ingress_id: undefined, port: 0 })} />
                    <Select label={t('inbounds.ingress')} allowDeselect={false} disabled={!member.node_id || !detail} value={member.ingress_id ? String(member.ingress_id) : ''} data={[{ value: '', label: t('inbounds.ingressDirect'), disabled: ingresses.some(g => g.require_ingress) }, ...ingresses.map(g => ({ value: String(g.id), label: g.name }))]} onChange={value => { const g = ingresses.find(item => String(item.id) === value); patchMember(group, member.key, { ingress_id: g?.id, port: g ? firstFreeIngressPort(g, detail?.inbounds.map(i => i.Port)) : member.port }) }} />
                  </SimpleGrid>
                  <Group align="flex-start" mt="xs" wrap="wrap">
                    <NumberInput style={{ flex: '1 1 150px' }} label={t('lines.listenPort')} min={1} max={65535} required value={member.port || ''} onChange={value => patchMember(group, member.key, { port: Number(value) })} />
                    {index > 0 && group.strategy === 'roundrobin' && <NumberInput style={{ flex: '1 1 90px' }} label={t('lines.weight')} min={1} max={100} value={member.weight} onChange={value => patchMember(group, member.key, { weight: Number(value) })} />}
                    <Group gap={3} mt={25}><ActionIcon variant="subtle" aria-label={t('lines.moveUp')} disabled={memberIndex === 0} onClick={() => patchGroup(group.key, { members: move(group.members, memberIndex, memberIndex - 1) })}><IconArrowUp size={16} /></ActionIcon><ActionIcon variant="subtle" aria-label={t('lines.moveDown')} disabled={memberIndex === group.members.length - 1} onClick={() => patchGroup(group.key, { members: move(group.members, memberIndex, memberIndex + 1) })}><IconArrowDown size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" aria-label={t('lines.removeMember')} disabled={group.members.length === 1} onClick={() => patchGroup(group.key, { members: group.members.filter(m => m.key !== member.key) })}><IconTrash size={16} /></ActionIcon></Group>
                  </Group>
                  {index > 0 && <Box mt="xs"><ForwardAddressModeSelect node={server} value={member.address_mode} label={t('forwardAddress.fromPreviousGroup')} onChange={address_mode => patchMember(group, member.key, { address_mode })} /></Box>}
                  {index > 0 && <Text size="xs" mt={4}>{t(member.address_mode === 'internal' ? 'forwardAddress.target' : 'forwardAddress.configuredTarget')}: <Code style={{ overflowWrap: 'anywhere' }}>{address || '—'}</Code></Text>}
                  {index > 0 && member.address_mode === 'internal' && address && draft.groups[index - 1].members.filter(source => source.node_id).map(source => <Box mt="xs" key={source.key}><Text size="xs" c="dimmed">{nodes.find(n => n.id === source.node_id)?.name} → {server?.name}</Text><ForwardConnectivityCheck sourceNodeID={source.node_id} sourceNodeName={nodes.find(n => n.id === source.node_id)?.name} target={address} protocol={protocol} /></Box>)}
                  {ingress && member.port > 0 && (index === 0 || member.address_mode !== 'internal') && <Text size="xs" mt={4} c={ingressPort(ingress, member.port) ? 'dimmed' : 'red'}>{ingressPort(ingress, member.port) ? t('lines.mappedPort', { port: ingressPort(ingress, member.port) }) : t('lines.unmappedPort')}</Text>}
                </Box>
              })}</Stack>
              <Button variant="subtle" size="xs" leftSection={<IconPlus size={14} />} mt="sm" disabled={group.members.length >= 8 || draft.backend !== 'relay' && index > 0} onClick={() => patchGroup(group.key, { members: [...group.members, newMember()], ...(index > 0 && protocol !== 'tcp' ? { strategy: 'roundrobin' } : {}) })}>{t('lines.addMember')}</Button>
            </Card>)}
            <Button variant="light" leftSection={<IconPlus size={16} />} disabled={draft.groups.length >= 8} onClick={() => setDraft(d => ({ ...d, groups: [...d.groups, newGroup()] }))}>{t('lines.addGroup')}</Button>
            <Group justify="space-between" align="flex-end"><Text size="sm" fw={500}>{t('lines.target')}</Text><Button size="xs" variant="light" disabled={!!createdInbound || loading} onClick={() => setCreatingInbound(true)}>{t('entryFlow.newInbound')}</Button></Group>
            <Select aria-label={t('lines.target')} searchable required value={draft.target_inbound_id ? String(draft.target_inbound_id) : null} data={choices.map(c => ({ value: String(c.ib.ID), label: `${c.node.name} / ${c.ib.Tag} (${c.ib.Protocol}:${c.ib.Port})${c.ib.Enabled ? '' : ` · ${t('common.disabled')}`}`, disabled: !c.ib.Enabled || members.some(m => m.node_id === c.ib.NodeID) }))} onChange={value => setDraft(d => ({ ...d, target_inbound_id: Number(value) }))} />
            <ForwardAddressModeSelect node={target?.node} value={draft.target_address_mode} onChange={target_address_mode => setDraft(d => ({ ...d, target_address_mode }))} />
            {target && <Text size="sm">{t(draft.target_address_mode === 'internal' ? 'forwardAddress.target' : 'forwardAddress.configuredTarget')}: <Code style={{ overflowWrap: 'anywhere' }}>{forwardTargetAddress(target, draft.target_address_mode) || '—'}</Code></Text>}
            {target && draft.target_address_mode === 'internal' && forwardTargetAddress(target, 'internal') && draft.groups.at(-1)?.members.filter(source => source.node_id).map(source => <Box key={source.key}><Text size="xs" c="dimmed">{nodes.find(n => n.id === source.node_id)?.name} → {target.node.name}</Text><ForwardConnectivityCheck sourceNodeID={source.node_id} sourceNodeName={nodes.find(n => n.id === source.node_id)?.name} target={forwardTargetAddress(target, 'internal')} protocol={protocol} /></Box>)}
            {(draft.target_address_mode === 'internal' || draft.groups.some((g, i) => i > 0 && g.members.some(m => m.address_mode === 'internal'))) && draft.backend !== 'relay' && <Alert color="yellow">{t(`forwardAddress.${draft.backend}Hint`)}</Alert>}
            {(options.isError || options.data?.error) && <Alert color="red"><Text size="sm">{options.data?.error || t('lines.protocolError')}</Text><Button variant="subtle" size="xs" onClick={() => options.refetch()}>{t('entryFlow.retry')}</Button></Alert>}
            {mixedFailover && <Alert color="yellow">{t('lines.mixedGroupHint')}</Alert>}
            {createdInbound && <Alert color="blue">{t('entryFlow.inboundSaved', { name: createdInbound.ib.Tag })}</Alert>}
            <Text size="xs" c="dimmed">{t('lines.targetHint')}</Text>
          </Stack></Box>
          <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={!valid || loading}>{t('common.save')}</Button></Group>
        </Stack>
      </form>
    </Modal>
    {creatingInbound && <EntryInboundCreator nodes={nodes.filter(n => !members.some(m => m.node_id === n.id))} onClose={() => setCreatingInbound(false)} onCreated={choice => { setCreatedInbound(choice); setCreatingInbound(false); setDraft(d => ({ ...d, target_inbound_id: choice.ib.ID })) }} />}
  </>
}
