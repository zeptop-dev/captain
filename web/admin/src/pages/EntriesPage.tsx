import { ActionIcon, Badge, Button, Card, Code, Group, Modal, NumberInput, Select, Stack, Switch, Table, Text, TextInput } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Entry, type Inbound, type Node } from '../lib/api'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

type Values = { Name: string; InboundID: string; DisplayHost: string; DisplayPort: number; Rate: number; Sort: number; Enabled: boolean }

export default function EntriesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['entries'], queryFn: () => api.get<Entry[]>('/api/admin/entries') })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const details = useQueries({ queries: (nodes.data ?? []).map((n) => ({ queryKey: ['node', String(n.id)], queryFn: () => api.get<{ node: Node; inbounds: Inbound[] }>(`/api/admin/nodes/${n.id}`) })) })
  const inbounds = details.flatMap((d) => (d.data ? d.data.inbounds.map((ib) => ({ ib, node: d.data!.node })) : []))
  const label = (id: number) => { const x = inbounds.find((i) => i.ib.ID === id); return x ? `${x.node.name} / ${x.ib.Tag} (${x.ib.Protocol}:${x.ib.Port})` : `#${id}` }
  const [editing, setEditing] = useState<Entry | 'new' | null>(null)
  const form = useForm<Values>({ initialValues: { Name: '', InboundID: '', DisplayHost: '', DisplayPort: 443, Rate: 1, Sort: 0, Enabled: true } })
  const payload = (v: Values) => ({ Name: v.Name, InboundID: Number(v.InboundID), DisplayHost: v.DisplayHost, DisplayPort: v.DisplayPort, Rate: v.Rate, Sort: v.Sort, Enabled: v.Enabled })
  const save = useMutation({ mutationFn: (v: Values) => editing === 'new' ? api.post('/api/admin/entries', payload(v)) : api.patch(`/api/admin/entries/${(editing as Entry).ID}`, payload(v)), onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); qc.invalidateQueries({ queryKey: ['entries'] }) }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/entries/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); qc.invalidateQueries({ queryKey: ['entries'] }) }, onError: toast.err })
  const openEdit = (e: Entry | 'new') => {
    if (e === 'new') form.setValues({ Name: '', InboundID: '', DisplayHost: '', DisplayPort: 443, Rate: 1, Sort: 0, Enabled: true })
    else form.setValues({ Name: e.Name, InboundID: String(e.InboundID), DisplayHost: e.DisplayHost, DisplayPort: e.DisplayPort, Rate: e.Rate, Sort: e.Sort, Enabled: e.Enabled })
    setEditing(e)
  }
  // Picking an inbound pre-fills the display address from the node.
  const onInbound = (id: string | null) => { form.setFieldValue('InboundID', id ?? ''); const x = inbounds.find((i) => String(i.ib.ID) === id); if (x && !form.values.DisplayHost) { const tls = x.ib.Settings?.tls as { mode?: number; server_name?: string } | undefined; form.setValues({ DisplayHost: (tls?.mode === 1 && tls.server_name) || x.node.public_addr || '', DisplayPort: x.ib.Port }) } }
  return (
    <>
      <PageHeader title={t('entries.title')} subtitle={t('entries.subtitle')} actions={<Button leftSection={<IconPlus size={16} />} onClick={() => openEdit('new')}>{t('entries.create')}</Button>} />
      <Card p={0}><Table>
        <Table.Thead><Table.Tr><Table.Th>{t('entries.name')}</Table.Th><Table.Th>{t('entries.inbound')}</Table.Th><Table.Th>{t('entries.displayHost')}</Table.Th><Table.Th>{t('entries.rate')}</Table.Th><Table.Th>{t('entries.enabled')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
        <Table.Tbody>
          {(q.data ?? []).map((e) => (
            <Table.Tr key={e.ID}>
              <Table.Td><Text fw={600}>{e.Name}</Text></Table.Td>
              <Table.Td><Text size="sm">{label(e.InboundID)}</Text></Table.Td>
              <Table.Td><Code>{e.DisplayHost}:{e.DisplayPort}</Code></Table.Td>
              <Table.Td>×{e.Rate}</Table.Td>
              <Table.Td>{e.Enabled ? <Badge color="teal">{t('common.enabled')}</Badge> : <Badge color="gray">{t('common.disabled')}</Badge>}</Table.Td>
              <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={() => openEdit(e)}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(e.ID) })}><IconTrash size={16} /></ActionIcon></Group></Table.Td>
            </Table.Tr>
          ))}
          {q.data?.length === 0 && <Table.Tr><Table.Td colSpan={6}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
        </Table.Tbody>
      </Table></Card>
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('entries.create') : t('common.edit')}>
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <TextInput label={t('entries.name')} required placeholder="🇯🇵 Tokyo IPLC" {...form.getInputProps('Name')} />
          <Select label={t('entries.inbound')} required searchable data={inbounds.map((i) => ({ value: String(i.ib.ID), label: label(i.ib.ID) }))} value={form.values.InboundID} onChange={onInbound} />
          <Group grow><TextInput label={t('entries.displayHost')} placeholder={t('entries.displayHostHint')} {...form.getInputProps('DisplayHost')} /><NumberInput label={t('entries.displayPort')} required min={1} max={65535} {...form.getInputProps('DisplayPort')} /></Group>
          <Group grow><NumberInput label={t('entries.rate')} min={0} step={0.1} decimalScale={2} {...form.getInputProps('Rate')} /><NumberInput label={t('entries.sort')} description={t('entries.sortHint')} {...form.getInputProps('Sort')} /></Group>
          <Switch label={t('entries.enabled')} {...form.getInputProps('Enabled', { type: 'checkbox' })} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(null)}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </>
  )
}
