import { Alert, Button, Card, Group, JsonInput, Loader, SimpleGrid, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'

const cores = ['xray', 'singbox', 'hysteria', 'mita'] as const
const labels: Record<string, string> = { xray: 'Xray', singbox: 'sing-box', hysteria: 'Hysteria', mita: 'mieru (mita)' }

// Per-core JSON objects deep-merged into the rendered config: the escape
// hatch for options the panel does not model. `load`/`save` differ per host
// (standalone API vs a Captain node endpoint).
export function OverridesCard({ queryKey, load, save, readOnly }: { queryKey: unknown[]; load: () => Promise<Record<string, string>>; save: (v: Record<string, string>) => Promise<unknown>; readOnly?: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey, queryFn: load })
  // Local edits overlay the loaded values; a fresh load replaces untouched fields.
  const [edits, setEdits] = useState<Record<string, string>>({})
  useSettingsDirty(!readOnly && Object.keys(edits).length > 0)
  const v = { ...q.data, ...edits }
  const setV = (f: (cur: Record<string, string>) => Record<string, string>) => setEdits((cur) => f({ ...q.data, ...cur }))
  const mut = useMutation({ mutationFn: () => save(v), onSuccess: () => { toast.ok(t('common.saved')); qc.setQueryData(queryKey, v); setEdits({}); qc.invalidateQueries({ queryKey }) }, onError: toast.err })
  if (q.isPending) return <Loader size="sm" />
  if (q.isError) return <Alert color="red">{t('workspace.loadError')} <Button variant="subtle" onClick={() => q.refetch()}>{t('workspace.retry')}</Button></Alert>
  return (
    <Card>
      <Title order={5} mb={4}>{t('overrides.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('overrides.hint')}</Text>
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="sm">
        {cores.map((c) => (
          <JsonInput key={c} label={labels[c]} placeholder={'{ }'} autosize minRows={3} maxRows={12} formatOnBlur validationError={t('overrides.invalid')} disabled={readOnly} value={v[c] ?? ''} onChange={(x) => setV((cur) => ({ ...cur, [c]: x }))} />
        ))}
      </SimpleGrid>
      {!readOnly && <Group justify="flex-end" mt="sm"><Button size="xs" loading={mut.isPending} disabled={Object.keys(edits).length === 0} onClick={() => mut.mutate()}>{t('common.save')}</Button></Group>}
    </Card>
  )
}
