import { ActionIcon, Badge, Box, Button, Card, Code, Group, Modal, Stack, Table, Text, Title, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconPencil, IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Ingress } from '../lib/api'
import { dnsToast, toast, type DNSResult } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'

import { IngressFields, emptyIngress, ingressPayload, ingressValues, type IngressValues } from './IngressFields'
export { IngressFields, emptyIngress, ingressPayload, type IngressValues } from './IngressFields'
import { ingressPortLabel } from '../lib/ingress'

// Line ingresses of one node (IPLC / dedicated NICs). Inbounds pick one;
// entries and relay forwards derive their addresses from it.
export function IngressesCard({ nodeID, ingresses, inbounds, embedded }: { nodeID: number; ingresses: Ingress[]; inbounds: { IngressID: number | null }[]; embedded?: boolean }) {
  // Embedded inside the node page's advanced section: no card frame, no title.
  const Root = embedded ? Box : Card
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [editing, setEditing] = useState<Ingress | 'new' | null>(null)
  const form = useForm<IngressValues>({ initialValues: emptyIngress })
  useSettingsDirty(editing !== null && form.isDirty())
  const invalidate = () => { qc.invalidateQueries({ queryKey: ['dns-health'] }); qc.invalidateQueries({ queryKey: ['dns-history'] }); qc.invalidateQueries({ queryKey: ['node', String(nodeID)] }); qc.invalidateQueries({ queryKey: ['node', nodeID] }) }
  const save = useMutation({ mutationFn: (v: IngressValues) => editing === 'new' ? api.post<{ dns?: DNSResult[] }>(`/api/admin/nodes/${nodeID}/ingresses`, ingressPayload(v)) : api.patch<{ dns?: DNSResult[] }>(`/api/admin/ingresses/${(editing as Ingress).id}`, ingressPayload(v)), onSuccess: (r) => { toast.ok(t('common.saved')); setEditing(null); invalidate(); dnsToast(r.dns) }, onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/ingresses/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); invalidate() }, onError: toast.err })
  const open = (g: Ingress | 'new') => { const values = g === 'new' ? emptyIngress : ingressValues(g); form.setValues(values); form.resetDirty(values); setEditing(g) }
  const close = () => {
    if (!form.isDirty()) { setEditing(null); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text>{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: () => setEditing(null) })
  }
  const uses = (id: number) => inbounds.filter((ib) => ib.IngressID === id).length
  return (
    <Root mb={embedded ? 0 : "lg"}>
      <Group justify={embedded ? 'flex-end' : 'space-between'} mb={4}>{!embedded && <Title order={5}>{t('ingress.title')}</Title>}<Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => open('new')}>{t('ingress.add')}</Button></Group>
      <Text size="xs" c="dimmed" mb="sm">{t('ingress.hint')}</Text>
      {ingresses.length > 0 && (
        <Table.ScrollContainer minWidth={680}><Table fz="sm"><Table.Thead><Table.Tr><Table.Th>{t('ingress.name')}</Table.Th><Table.Th>{t('ingress.bindIP')}</Table.Th><Table.Th>{t('ingress.lineIP')}</Table.Th><Table.Th>{t('ingress.entryHost')}</Table.Th><Table.Th>{t('ingress.ports')}</Table.Th><Table.Th>{t('ingress.inbounds')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {ingresses.map((g) => (
              <Table.Tr key={g.id}>
                <Table.Td><Text fw={600}>{g.name}</Text></Table.Td>
                <Table.Td>{g.bind_ip ? <Code>{g.bind_ip}</Code> : <Text size="xs" c="dimmed">{t('ingress.anyAddr')}</Text>}</Table.Td>
                <Table.Td>{g.line_ip ? <Code>{g.line_ip}</Code> : '—'}</Table.Td>
                <Table.Td>{g.entry_host ? <><Code>{g.entry_host}</Code>{g.entry_domain && <Text size="xs" c="dimmed">{g.entry_domain}</Text>}</> : <Tooltip label={t('ingress.noEntryHint')}><Badge size="xs" color="orange" variant="light">{t('ingress.noEntry')}</Badge></Tooltip>}</Table.Td>
                <Table.Td><Text size="xs">{ingressPortLabel(g) || t('ingress.anyPort')}</Text></Table.Td>
                <Table.Td><Text size="xs">{uses(g.id)}</Text></Table.Td>
                <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={() => open(g)}><IconPencil size={14} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('ingress.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate(g.id) })}><IconTrash size={14} /></ActionIcon></Group></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody></Table></Table.ScrollContainer>
      )}
      <Modal opened={editing !== null} onClose={close} title={editing === 'new' ? t('ingress.add') : t('common.edit')} size="lg" closeOnClickOutside={false}>
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack>
          <IngressFields form={form} />
          <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>
    </Root>
  )
}
