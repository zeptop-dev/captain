import { ActionIcon, Badge, Button, Card, Code, Group, Modal, NumberInput, Stack, Table, Text, TextInput, Title, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Ingress } from '../lib/api'
import { toast } from '../lib/notify'

export type IngressValues = { Name: string; BindIP: string; LineIP: string; EntryHost: string; PortFrom: number | string; PortTo: number | string; PortOffset: number | string }
export const emptyIngress: IngressValues = { Name: 'IPLC', BindIP: '', LineIP: '', EntryHost: '', PortFrom: '', PortTo: '', PortOffset: 0 }
export const ingressPayload = (v: IngressValues) => ({ Name: v.Name, BindIP: v.BindIP, LineIP: v.LineIP, EntryHost: v.EntryHost, PortFrom: Number(v.PortFrom) || 0, PortTo: Number(v.PortTo) || 0, PortOffset: Number(v.PortOffset) || 0 })

// The fields of one line ingress, shared by the card and the inbound recipe.
export function IngressFields({ form }: { form: ReturnType<typeof useForm<IngressValues>> }) {
  const { t } = useTranslation()
  return (
    <>
      <Group grow>
        <TextInput label={t('ingress.name')} required {...form.getInputProps('Name')} />
        <TextInput label={t('ingress.bindIP')} description={t('ingress.bindIPHint')} placeholder="10.10.0.2" {...form.getInputProps('BindIP')} />
      </Group>
      <Group grow>
        <TextInput label={t('ingress.lineIP')} description={t('ingress.lineIPHint')} placeholder="198.51.100.20" {...form.getInputProps('LineIP')} />
        <TextInput label={t('ingress.entryHost')} description={t('ingress.entryHostHint')} placeholder="203.0.113.30" {...form.getInputProps('EntryHost')} />
      </Group>
      <Group grow>
        <NumberInput label={t('ingress.portFrom')} min={1} max={65535} placeholder="17701" {...form.getInputProps('PortFrom')} />
        <NumberInput label={t('ingress.portTo')} min={1} max={65535} placeholder="17799" {...form.getInputProps('PortTo')} />
        <NumberInput label={t('ingress.portOffset')} description={t('ingress.portOffsetHint')} {...form.getInputProps('PortOffset')} />
      </Group>
    </>
  )
}

// Line ingresses of one node (IPLC / dedicated NICs). Inbounds pick one;
// entries and relay forwards derive their addresses from it.
export function IngressesCard({ nodeID, ingresses, inbounds }: { nodeID: number; ingresses: Ingress[]; inbounds: { IngressID: number | null }[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [editing, setEditing] = useState<Ingress | 'new' | null>(null)
  const form = useForm<IngressValues>({ initialValues: emptyIngress })
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['node', String(nodeID)] }); qc.invalidateQueries({ queryKey: ['node', nodeID] }) }
  const save = useMutation({ mutationFn: (v: IngressValues) => editing === 'new' ? api.post(`/api/admin/nodes/${nodeID}/ingresses`, ingressPayload(v)) : api.patch(`/api/admin/ingresses/${(editing as Ingress).id}`, ingressPayload(v)), onSuccess: () => { toast.ok(t('common.saved')); setEditing(null); invalidate() }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/ingresses/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const open = (g: Ingress | 'new') => { form.setValues(g === 'new' ? emptyIngress : { Name: g.name, BindIP: g.bind_ip, LineIP: g.line_ip, EntryHost: g.entry_host, PortFrom: g.port_from || '', PortTo: g.port_to || '', PortOffset: g.port_offset }); setEditing(g) }
  const uses = (id: number) => inbounds.filter((ib) => ib.IngressID === id).length
  return (
    <Card mb="lg">
      <Group justify="space-between" mb={4}><Title order={5}>{t('ingress.title')}</Title><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => open('new')}>{t('ingress.add')}</Button></Group>
      <Text size="xs" c="dimmed" mb="sm">{t('ingress.hint')}</Text>
      {ingresses.length > 0 && (
        <Table fz="sm"><Table.Thead><Table.Tr><Table.Th>{t('ingress.name')}</Table.Th><Table.Th>{t('ingress.bindIP')}</Table.Th><Table.Th>{t('ingress.lineIP')}</Table.Th><Table.Th>{t('ingress.entryHost')}</Table.Th><Table.Th>{t('ingress.ports')}</Table.Th><Table.Th>{t('ingress.inbounds')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {ingresses.map((g) => (
              <Table.Tr key={g.id}>
                <Table.Td><Text fw={600}>{g.name}</Text></Table.Td>
                <Table.Td>{g.bind_ip ? <Code>{g.bind_ip}</Code> : <Text size="xs" c="dimmed">{t('ingress.anyAddr')}</Text>}</Table.Td>
                <Table.Td>{g.line_ip ? <Code>{g.line_ip}</Code> : '—'}</Table.Td>
                <Table.Td>{g.entry_host ? <Code>{g.entry_host}</Code> : <Tooltip label={t('ingress.noEntryHint')}><Badge size="xs" color="orange" variant="light">{t('ingress.noEntry')}</Badge></Tooltip>}</Table.Td>
                <Table.Td><Text size="xs">{g.port_from ? `${g.port_from}–${g.port_to}` : t('ingress.anyPort')}{g.port_offset ? ` (${g.port_offset > 0 ? '+' : ''}${g.port_offset})` : ''}</Text></Table.Td>
                <Table.Td><Text size="xs">{uses(g.id)}</Text></Table.Td>
                <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={() => open(g)}><IconPencil size={14} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('ingress.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(g.id) })}><IconTrash size={14} /></ActionIcon></Group></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody></Table>
      )}
      <Modal opened={editing !== null} onClose={() => setEditing(null)} title={editing === 'new' ? t('ingress.add') : t('common.edit')} size="lg">
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <IngressFields form={form} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditing(null)}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </Card>
  )
}
