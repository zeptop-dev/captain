import { Button, Card, Group, NumberInput, Progress, Select, SimpleGrid, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { bytes } from '../lib/format'
import { toast } from '../lib/notify'

interface NodeProbe {
  probe: { node_id: number; hidden: boolean; info: { region?: string; provider?: string; provider_url?: string; price?: string; expires_at?: string; note?: string }; limit_bytes: number; reset_day: number; mode: string; period_start: string; used_up: number; used_down: number; prev_used: number }
  billed: number
  live?: { at: string; host: Record<string, unknown> }
}

// Per-node probe settings: display facts for the status page, hide flag,
// and the monthly NIC traffic allowance with its current usage.
export function NodeProbeCard({ nodeID }: { nodeID: number }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['node-probe', nodeID], queryFn: () => api.get<NodeProbe>(`/api/admin/nodes/${nodeID}/probe`), refetchInterval: 30_000 })
  const form = useForm({ initialValues: { Hidden: false, region: '', provider: '', provider_url: '', price: '', expires_at: '', note: '', LimitGB: 0, ResetDay: 1, Mode: 'sum' } })
  useEffect(() => { const p = q.data?.probe; if (p) form.setValues({ Hidden: p.hidden, region: p.info.region ?? '', provider: p.info.provider ?? '', provider_url: p.info.provider_url ?? '', price: p.info.price ?? '', expires_at: p.info.expires_at ?? '', note: p.info.note ?? '', LimitGB: +(p.limit_bytes / 2 ** 30).toFixed(0), ResetDay: p.reset_day, Mode: p.mode }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.put(`/api/admin/nodes/${nodeID}/probe`, { Hidden: v.Hidden, Info: { region: v.region.toUpperCase(), provider: v.provider, provider_url: v.provider_url, price: v.price, expires_at: v.expires_at, note: v.note }, LimitBytes: Math.round(v.LimitGB * 2 ** 30), ResetDay: v.ResetDay, Mode: v.Mode }), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['node-probe', nodeID] }) }, onError: toast.err })
  const reset = useMutation({ mutationFn: () => api.post(`/api/admin/nodes/${nodeID}/probe/reset-traffic`), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['node-probe', nodeID] }) }, onError: toast.err })
  const p = q.data?.probe
  const used = q.data?.billed ?? 0
  const pct = p && p.limit_bytes > 0 ? Math.min(100, Math.round((used / p.limit_bytes) * 100)) : 0
  return (
    <Card mb="lg">
      <Title order={5} mb="xs">{t('nodeProbe.title')}</Title>
      {p && (
        <Stack gap={4} mb="sm">
          <Group justify="space-between"><Text size="sm">{t('nodeProbe.used')}: <b>{bytes(used)}</b>{p.limit_bytes > 0 && <> / {bytes(p.limit_bytes)}</>}</Text><Text size="xs" c="dimmed">{t('nodeProbe.prev')}: {bytes(p.prev_used)}</Text></Group>
          {p.limit_bytes > 0 && <Progress value={pct} color={pct > 90 ? 'red' : pct > 70 ? 'orange' : 'teal'} />}
          <Text size="xs" c="dimmed">↑ {bytes(p.used_up)} · ↓ {bytes(p.used_down)} · {t('nodeProbe.since')} {p.period_start ? new Date(p.period_start).toLocaleDateString() : '—'}</Text>
        </Stack>
      )}
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Stack gap="sm">
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <NumberInput label={t('nodeProbe.limit')} description={t('nodeProbe.limitHint')} min={0} {...form.getInputProps('LimitGB')} />
          <NumberInput label={t('nodeProbe.resetDay')} min={1} max={28} {...form.getInputProps('ResetDay')} />
          <Select label={t('nodeProbe.mode')} data={[{ value: 'sum', label: t('nodeProbe.modes.sum') }, { value: 'up', label: t('nodeProbe.modes.up') }, { value: 'down', label: t('nodeProbe.modes.down') }, { value: 'max', label: t('nodeProbe.modes.max') }]} allowDeselect={false} {...form.getInputProps('Mode')} />
        </SimpleGrid>
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <TextInput label={t('nodeProbe.region')} placeholder="JP" maxLength={2} {...form.getInputProps('region')} />
          <TextInput label={t('nodeProbe.provider')} placeholder="Vultr" {...form.getInputProps('provider')} />
          <TextInput label={t('nodeProbe.providerURL')} placeholder="https://…" {...form.getInputProps('provider_url')} />
          <TextInput label={t('nodeProbe.price')} placeholder="$5 / mo" {...form.getInputProps('price')} />
          <TextInput label={t('nodeProbe.expires')} placeholder="2027-01-31" {...form.getInputProps('expires_at')} />
          <TextInput label={t('nodeProbe.note')} {...form.getInputProps('note')} />
        </SimpleGrid>
        <Group justify="space-between">
          <Switch label={t('nodeProbe.hidden')} {...form.getInputProps('Hidden', { type: 'checkbox' })} />
          <Group gap="xs"><Button size="xs" variant="subtle" color="gray" onClick={() => reset.mutate()}>{t('nodeProbe.reset')}</Button><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Group>
      </Stack></form>
    </Card>
  )
}
