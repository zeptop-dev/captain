import { ActionIcon, Alert, Button, Group, NumberInput, Select, SimpleGrid, Stack, Text, TextInput } from '@mantine/core'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'

interface Upstream { cidr: string; protocol: string; port: number }
interface Policy { upstreams: Upstream[]; supported: boolean; minimum_version?: string }

export function EgressUpstreams({ endpoint }: { endpoint: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['egress', endpoint], queryFn: () => api.get<Policy>(endpoint) })
  const [draft, setDraft] = useState<Upstream[] | null>(null)
  useSettingsDirty(draft !== null)
  const list = draft ?? q.data?.upstreams ?? []
  const save = useMutation({ mutationFn: () => api.put<Policy>(endpoint, { upstreams: list }), onSuccess: value => { qc.setQueryData(['egress', endpoint], value); setDraft(null); toast.ok(t('common.saved')) }, onError: toast.err })
  const patch = (index: number, value: Partial<Upstream>) => setDraft(list.map((u, i) => i === index ? { ...u, ...value } : u))
  return <Stack gap="sm" mt="md">
    <Text fw={600} size="sm">{t('egress.title')}</Text>
    <Text size="xs" c="dimmed">{t('egress.hint')}</Text>
    {q.isError && <Alert color="red">{t('egress.loadFailed')} <Button size="xs" variant="subtle" onClick={() => q.refetch()}>{t('egress.retry')}</Button></Alert>}
    {q.data && !q.data.supported && <Alert color="yellow">{t('egress.requiresVersion', { version: q.data.minimum_version })}</Alert>}
    {list.map((u, i) => <SimpleGrid key={i} cols={{ base: 1, sm: 3 }}>
      <TextInput label={t('egress.address')} placeholder="10.10.0.2/32" value={u.cidr} disabled={save.isPending} onChange={e => patch(i, { cidr: e.currentTarget.value })} />
      <Select label={t('forwards.protocol')} data={['tcp', 'udp']} allowDeselect={false} value={u.protocol} disabled={save.isPending} onChange={v => patch(i, { protocol: v || 'tcp' })} />
      <Group align="flex-end" wrap="nowrap"><NumberInput label={t('egress.port')} min={1} max={65535} value={u.port || ''} disabled={save.isPending} style={{ flex: 1, minWidth: 0 }} onChange={v => patch(i, { port: Number(v) || 0 })} /><ActionIcon aria-label={t('common.delete')} variant="subtle" color="red" mb={5} disabled={save.isPending} onClick={() => setDraft(list.filter((_, j) => i !== j))}><IconTrash size={16} /></ActionIcon></Group>
    </SimpleGrid>)}
    <Group justify="space-between"><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} disabled={!q.data?.supported || list.length >= 64 || save.isPending} onClick={() => setDraft([...list, { cidr: '', protocol: 'tcp', port: 1080 }])}>{t('egress.add')}</Button><Group><Button size="xs" variant="default" disabled={draft === null || save.isPending} onClick={() => setDraft(null)}>{t('common.cancel')}</Button><Button size="xs" loading={save.isPending} disabled={draft === null || !q.data || q.isError || (!q.data.supported && list.length > 0) || list.some(u => !u.cidr.trim() || u.port < 1 || u.port > 65535)} onClick={() => save.mutate()}>{t('common.save')}</Button></Group></Group>
    <Text size="xs" c="dimmed">{t('egress.applyHint')}</Text>
  </Stack>
}
