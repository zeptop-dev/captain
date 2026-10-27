import { useSettingsDirty } from '../lib/settings-draft'
import { ActionIcon, Button, Group, Modal, Stack, Text, Textarea, TextInput } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'

export function MetadataEditor({ endpoint }: { endpoint: string }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['metadata', endpoint], queryFn: () => api.get<Record<string, string>>(endpoint) })
  const [opened, setOpened] = useState(false)
  const [rows, setRows] = useState<[string, string][]>([])
  const [baseline, setBaseline] = useState<[string, string][]>([])
  useSettingsDirty(opened && JSON.stringify(rows) !== JSON.stringify(baseline))
  const save = useMutation({ mutationFn: () => api.put(endpoint, Object.fromEntries(rows)), onSuccess: () => { setOpened(false); qc.invalidateQueries({ queryKey: ['metadata', endpoint] }); toast.ok(t('common.saved')) }, onError: toast.err })
  const valid = rows.every(([key, value]) => key.trim() && key.length <= 80 && value.length <= 2048) && new Set(rows.map(r => r[0])).size === rows.length
  return <Stack gap="xs">
    <Group justify="space-between"><Text fw={600}>{t('metadata.title')}</Text>{me?.role !== 'support' && <Button size="compact-xs" variant="subtle" disabled={!q.data} onClick={() => { setRows(Object.entries(q.data ?? {})); setBaseline(Object.entries(q.data ?? {})); setOpened(true) }}>{t('common.edit')}</Button>}</Group>
    <Text size="xs" c="dimmed">{t('metadata.hint')}</Text>
    {Object.entries(q.data ?? {}).map(([key, value]) => <div key={key}><Text size="xs" c="dimmed">{key}</Text><Text size="sm" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{value}</Text></div>)}
    <Modal opened={opened} onClose={() => { if (!save.isPending) setOpened(false) }} title={t('metadata.title')} size="lg">
      <fieldset disabled={save.isPending} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}><Stack>{rows.map(([key, value], i) => <Group key={i} align="flex-start" wrap="nowrap">
        <TextInput label={t('metadata.key')} value={key} maxLength={80} onChange={e => setRows(rows.map((r, n) => n === i ? [e.currentTarget.value, r[1]] : r))} style={{ flex: 1 }} />
        <Textarea label={t('metadata.value')} value={value} maxLength={2048} autosize minRows={1} onChange={e => setRows(rows.map((r, n) => n === i ? [r[0], e.currentTarget.value] : r))} style={{ flex: 2 }} />
        <ActionIcon mt={28} color="red" variant="subtle" aria-label={t('common.delete')} onClick={() => setRows(rows.filter((_, n) => n !== i))}><IconTrash size={16} /></ActionIcon>
      </Group>)}<Group justify="space-between"><Button variant="default" disabled={rows.length >= 32} onClick={() => setRows([...rows, ['', '']])}>{t('metadata.add')}</Button><Button disabled={!valid} loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button></Group></Stack></fieldset>
    </Modal>
  </Stack>
}
