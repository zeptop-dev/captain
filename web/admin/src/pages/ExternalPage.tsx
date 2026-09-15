import { ActionIcon, Badge, Button, Card, Code, Group, Modal, NumberInput, Select, Stack, Switch, Table, Text, TextInput, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Group as UGroup } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Source { id: number; name: string; url: string; user_agent: string; group_id: number | null; rate: number; enabled: boolean; last_sync_at: string | null; last_error: string; nodes: number }
interface ExtNode { id: number; source_id: number | null; name: string; uri: string; group_id: number | null; rate: number; sort: number; enabled: boolean; probed_at: string | null; probe_ms: number; probe_error: string }

// External nodes: share links from other providers offered in our
// subscriptions. Traffic through them is not accounted (no agent).
export default function ExternalPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const sources = useQuery({ queryKey: ['ext-sources'], queryFn: () => api.get<Source[]>('/api/admin/external/sources') })
  const nodes = useQuery({ queryKey: ['ext-nodes'], queryFn: () => api.get<ExtNode[]>('/api/admin/external/nodes') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const groupData = [{ value: '', label: t('inbounds.groupAll') }, ...(groups.data ?? []).map((g) => ({ value: String(g.ID), label: g.Name }))]
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['ext-sources'] }); qc.invalidateQueries({ queryKey: ['ext-nodes'] }) }
  const [addingSrc, setAddingSrc] = useState(false)
  const [importing, setImporting] = useState(false)
  const srcForm = useForm({ initialValues: { name: '', url: '', user_agent: 'v2rayN/7.0', group: '', rate: 1, enabled: true, hide_dead: false } })
  const probe = useMutation({ mutationFn: () => api.post<{ up: number; total: number }>('/api/admin/external/probe'), onSuccess: (r) => { toast.ok(t('external.probed', { up: r.up, total: r.total })); invalidate() }, onError: toast.err })
  const saveSrc = useMutation({ mutationFn: (v: typeof srcForm.values) => api.post<{ synced: number; error: string }>('/api/admin/external/sources', { name: v.name, url: v.url, user_agent: v.user_agent, group_id: v.group ? Number(v.group) : null, rate: v.rate, hide_dead: v.hide_dead, enabled: v.enabled }), onSuccess: (r) => { r.error ? toast.err(new Error(r.error)) : toast.ok(t('external.synced', { count: r.synced })); setAddingSrc(false); srcForm.reset(); invalidate() }, onError: toast.err })
  const sync = useMutation({ mutationFn: (id: number) => api.post<{ synced: number }>(`/api/admin/external/sources/${id}/sync`), onSuccess: (r) => { toast.ok(t('external.synced', { count: r.synced })); invalidate() }, onError: toast.err })
  const delSrc = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/external/sources/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const impForm = useForm({ initialValues: { text: '', group: '', rate: 1 } })
  const imp = useMutation({ mutationFn: (v: typeof impForm.values) => api.post<{ added: number; skipped: number }>('/api/admin/external/nodes', { Text: v.text, GroupID: v.group ? Number(v.group) : null, Rate: v.rate }), onSuccess: (r) => { toast.ok(t('external.imported', { added: r.added, skipped: r.skipped })); setImporting(false); impForm.reset(); invalidate() }, onError: toast.err })
  const upd = useMutation({ mutationFn: (n: ExtNode) => api.patch(`/api/admin/external/nodes/${n.id}`, n), onSuccess: invalidate, onError: toast.err })
  const delNode = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/external/nodes/${id}`), onSuccess: invalidate, onError: toast.err })
  const srcName = (id: number | null) => sources.data?.find((s) => s.id === id)?.name ?? t('external.manual')
  return (
    <>
      <PageHeader title={t('external.title')} subtitle={t('external.subtitle')} actions={<>
        <Button variant="default" loading={probe.isPending} onClick={() => probe.mutate()}>{t('external.probe')}</Button>
        <Button variant="default" leftSection={<IconPlus size={16} />} onClick={() => setAddingSrc(true)}>{t('external.addSource')}</Button>
        <Button leftSection={<IconPlus size={16} />} onClick={() => setImporting(true)}>{t('external.import')}</Button>
      </>} />
      {(sources.data ?? []).length > 0 && (
        <Card p={0} mb="lg"><Table>
          <Table.Thead><Table.Tr><Table.Th>{t('external.source')}</Table.Th><Table.Th>URL</Table.Th><Table.Th>{t('external.nodes')}</Table.Th><Table.Th>{t('external.lastSync')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {sources.data!.map((s) => (
              <Table.Tr key={s.id}>
                <Table.Td><Text size="sm" fw={600}>{s.name}</Text>{!s.enabled && <Badge size="xs" color="gray">{t('common.disabled')}</Badge>}</Table.Td>
                <Table.Td><Text size="xs" c="dimmed" truncate maw={320}>{s.url}</Text></Table.Td>
                <Table.Td>{s.nodes}</Table.Td>
                <Table.Td><Text size="xs" c={s.last_error ? 'red' : 'dimmed'}>{s.last_error || (s.last_sync_at ? when(s.last_sync_at) : '—')}</Text></Table.Td>
                <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">
                  <ActionIcon variant="subtle" color="gray" loading={sync.isPending} onClick={() => sync.mutate(s.id)}><IconRefresh size={16} /></ActionIcon>
                  <ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('external.deleteSourceHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => delSrc.mutate(s.id) })}><IconTrash size={16} /></ActionIcon>
                </Group></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table></Card>
      )}
      <Card p={0}><Table.ScrollContainer minWidth={720}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('external.name')}</Table.Th><Table.Th>{t('external.link')}</Table.Th><Table.Th>{t('external.from')}</Table.Th><Table.Th>{t('external.reach')}</Table.Th><Table.Th>{t('inbounds.group')}</Table.Th><Table.Th>{t('entries.rate')}</Table.Th><Table.Th>{t('common.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(nodes.data ?? []).map((n) => (
            <Table.Tr key={n.id}>
              <Table.Td><Text size="sm" fw={600}>{n.name}</Text></Table.Td>
              <Table.Td><Code style={{ display: 'inline-block', maxWidth: 260, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', verticalAlign: 'middle' }}>{n.uri.split('#')[0]}</Code></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{srcName(n.source_id)}</Text></Table.Td>
              <Table.Td>{!n.probed_at ? <Text size="xs" c="dimmed">—</Text> : n.probe_error ? <Badge size="xs" color="red" variant="light" title={n.probe_error}>{t('external.down')}</Badge> : <Badge size="xs" color="teal" variant="light">{Math.round(n.probe_ms)} ms</Badge>}</Table.Td>
              <Table.Td><Select size="xs" data={groupData} value={n.group_id ? String(n.group_id) : ''} allowDeselect={false} onChange={(v) => upd.mutate({ ...n, group_id: v ? Number(v) : null })} w={140} /></Table.Td>
              <Table.Td><NumberInput size="xs" w={80} min={0} step={0.1} decimalScale={2} value={n.rate} onBlur={(e) => { const v = Number(e.currentTarget.value); if (v !== n.rate) upd.mutate({ ...n, rate: v }) }} /></Table.Td>
              <Table.Td><Switch size="xs" checked={n.enabled} onChange={(e) => upd.mutate({ ...n, enabled: e.currentTarget.checked })} /></Table.Td>
              <Table.Td><ActionIcon variant="subtle" color="red" onClick={() => delNode.mutate(n.id)}><IconTrash size={16} /></ActionIcon></Table.Td>
            </Table.Tr>
          ))}
          {(nodes.data ?? []).length === 0 && <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" ta="center" py="lg">{t('external.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Table.ScrollContainer></Card>

      <Modal opened={addingSrc} onClose={() => setAddingSrc(false)} title={t('external.addSource')}>
        <form onSubmit={srcForm.onSubmit((v) => saveSrc.mutate(v))}><Stack>
          <TextInput label={t('external.name')} required {...srcForm.getInputProps('name')} />
          <TextInput label={t('external.subUrl')} description={t('external.subUrlHint')} required placeholder="https://airport.example/api/v1/client/subscribe?token=…" {...srcForm.getInputProps('url')} />
          <Group grow><TextInput label="User-Agent" {...srcForm.getInputProps('user_agent')} /><Select label={t('inbounds.group')} data={groupData} allowDeselect={false} {...srcForm.getInputProps('group')} /><NumberInput label={t('entries.rate')} min={0} step={0.1} decimalScale={2} {...srcForm.getInputProps('rate')} /></Group>
          <Switch label={t('external.hideDead')} description={t('external.hideDeadHint')} {...srcForm.getInputProps('hide_dead', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button type="submit" loading={saveSrc.isPending}>{t('external.saveAndSync')}</Button></Group>
        </Stack></form>
      </Modal>
      <Modal opened={importing} onClose={() => setImporting(false)} title={t('external.import')} size="lg">
        <form onSubmit={impForm.onSubmit((v) => imp.mutate(v))}><Stack>
          <Textarea label={t('external.links')} description={t('external.linksHint')} autosize minRows={6} ff="monospace" required {...impForm.getInputProps('text')} />
          <Group grow><Select label={t('inbounds.group')} data={groupData} allowDeselect={false} {...impForm.getInputProps('group')} /><NumberInput label={t('entries.rate')} min={0} step={0.1} decimalScale={2} {...impForm.getInputProps('rate')} /></Group>
          <Group justify="flex-end"><Button type="submit" loading={imp.isPending}>{t('external.import')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}
