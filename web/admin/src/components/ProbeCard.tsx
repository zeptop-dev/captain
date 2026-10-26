import { SettingsFields } from './SettingsFields'
import { SettingsLoadState } from './SettingsLoadState'
import { ActionIcon, Button, Card, Code, Group, MultiSelect, NumberInput, Select, Stack, Switch, Table, Text, TextInput, Title, Textarea } from '@mantine/core'
import { useSettingsForm as useForm } from '../lib/settings-draft'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Node } from '../lib/api'
import { validBrandAssetInput } from '../lib/branding'
import { BrandAssetInput } from './BrandAssetInput'
import { ProbeAppearanceFields } from './ProbeAppearanceFields'
import { toast } from '../lib/notify'
import { currencyOptions } from '../lib/currency-options'
import { carrierProbeDraft, carrierProbePayload, carrierTargetPlaceholder, type CarrierProbe, type CarrierTargetTexts, type CarrierFamily } from '../lib/carrier-probe'

interface ProbeSettings {
  follow_site_branding?: boolean; logo_dark?: string; favicon?: string
  appearance?: { preset: string; scheme: string }
  carrier_probe?: CarrierProbe
  finance_currency?: string
  enabled: boolean; page_enabled?: boolean; public_sections?: string[]; layout?: string; beat_seconds: number; carrier_ping: boolean; carriers?: { name: string; addr: string }[]; path: string; hosts: string[]; visibility: string; title: string; logo: string; show_globe: boolean; show_ip: boolean
  alerts: { offline_seconds: number; cpu_pct: number; mem_pct: number; disk_pct: number; window_minutes: number; traffic: boolean }
}
interface PingTask { id: number; name: string; type: string; target: string; interval_seconds: number; node_ids: number[] | null; enabled: boolean }

// Status page (probe) settings: master switch, address (path and/or
// dedicated hosts), visibility, appearance, alert thresholds, ping tasks.
export function ProbeCard() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const [showIndependentBrandErrors, setShowIndependentBrandErrors] = useState(false)
  const q = useQuery({ queryKey: ['probe-settings'], queryFn: () => api.get<ProbeSettings>('/api/admin/settings/probe') })
  const form = useForm<ProbeSettings & { hostsText: string; carrier_probe: CarrierProbe; carrierTargetsText: CarrierTargetTexts }>({ initialValues: { follow_site_branding: false, logo_dark: '', favicon: '', finance_currency: 'CNY', appearance: { preset: 'inherit', scheme: 'inherit' }, enabled: false, page_enabled: true, public_sections: ['cpu', 'memory', 'disk', 'network', 'system', 'info', 'traffic', 'latency', 'history', 'availability'], layout: 'grid', beat_seconds: 10, carrier_ping: true, carrier_probe: carrierProbeDraft().probe, carrierTargetsText: carrierProbeDraft().texts, path: '/status', hosts: [], hostsText: '', visibility: 'public', title: '', logo: '', show_globe: false, show_ip: false, alerts: { offline_seconds: 180, cpu_pct: 0, mem_pct: 0, disk_pct: 0, window_minutes: 5, traffic: true } }, validate: { logo: value => validBrandAssetInput(value) ? null : t('branding.invalidURL'), logo_dark: value => validBrandAssetInput(value) ? null : t('branding.invalidURL'), favicon: value => validBrandAssetInput(value) ? null : t('branding.invalidURL') } })
  useEffect(() => { if (q.data) form.hydrate({ ...q.data, follow_site_branding: q.data.follow_site_branding ?? false, logo_dark: q.data.logo_dark ?? '', favicon: q.data.favicon ?? '', finance_currency: q.data.finance_currency ?? 'CNY', appearance: q.data.appearance ?? { preset: 'inherit', scheme: 'inherit' }, public_sections: q.data.public_sections ?? ['cpu', 'memory', 'disk', 'network', 'system', 'info', 'traffic', 'latency', 'history', 'availability'], layout: q.data.layout || 'grid', page_enabled: q.data.page_enabled ?? true, hostsText: (q.data.hosts ?? []).join(', '), carrier_probe: carrierProbeDraft(q.data.carrier_probe, q.data.carriers).probe, carrierTargetsText: carrierProbeDraft(q.data.carrier_probe, q.data.carriers).texts }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (v: typeof form.values) => { const { hostsText, carrierTargetsText, ...settings } = v; const carrier_probe = carrierProbePayload(v.carrier_probe, carrierTargetsText); return api.put('/api/admin/settings/probe', { ...settings, hosts: hostsText.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean), carrier_probe, carriers: carrier_probe.tcp_ipv4 }) }, onSuccess: () => { form.resetDirty(); setShowIndependentBrandErrors(false); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['probe-settings'] }) }, onError: toast.err })
  const v = form.values
  const origin = window.location.origin
  if (q.data === undefined) return <SettingsLoadState query={q} />
  return (
    <Card>
      <Title order={5} mb="xs">{t('probe.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('probe.hint')}</Text>
      <form onSubmit={form.onSubmit((vals) => save.mutate(vals), errors => { if (errors.logo || errors.logo_dark || errors.favicon) setShowIndependentBrandErrors(true) })}><Stack gap="sm">
        <SettingsFields>
          <Switch label={t('probe.collection')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
          <NumberInput label={t('probe.beat')} min={3} max={300} {...form.getInputProps('beat_seconds')} />
          <Switch label={t('probe.carrier')} {...form.getInputProps('carrier_ping', { type: 'checkbox' })} />
        </SettingsFields>
        <Switch label={t('probe.pageEnabled')} description={t('probe.pageEnabledHint')} {...form.getInputProps('page_enabled', { type: 'checkbox' })} />
        {v.carrier_ping && <Stack gap="xs">
          <SettingsFields>
            <Select label={t('probe.carrierProtocol')} data={['tcp', 'icmp'].map(value => ({ value, label: value.toUpperCase() }))} allowDeselect={false} {...form.getInputProps('carrier_probe.protocol')} />
            <Select label={t('probe.carrierFamily')} data={[{ value: 'ipv4', label: 'IPv4' }, { value: 'ipv6', label: 'IPv6' }, { value: 'dual', label: 'IPv4 + IPv6' }]} allowDeselect={false} {...form.getInputProps('carrier_probe.family')} />
          </SettingsFields>
          <Text size="xs" c="dimmed">{t('probe.carrierModesHint')}</Text>
          <SettingsFields>
            {(['ipv4', 'ipv6'] as CarrierFamily[]).filter(family => v.carrier_probe.family === 'dual' || v.carrier_probe.family === family).map(family => <Textarea key={`${v.carrier_probe.protocol}_${family}`} label={`${t('probe.carriers')} · ${family === 'ipv4' ? 'IPv4' : 'IPv6'}`} description={v.carrier_probe.protocol === 'icmp' ? t('probe.carriersICMPHint') : t('probe.carriersHint')} autosize minRows={3} placeholder={carrierTargetPlaceholder(v.carrier_probe.protocol, family)} styles={{ input: { fontFamily: 'monospace', fontSize: 12 } }} {...form.getInputProps(`carrierTargetsText.${v.carrier_probe.protocol}_${family}`)} />)}
          </SettingsFields>
        </Stack>}
        <SettingsFields>
          <TextInput label={t('probe.path')} description={t('probe.pathHint')} placeholder="/status" {...form.getInputProps('path')} />
          <TextInput label={t('probe.hosts')} description={t('probe.hostsHint')} placeholder="status.example.com, probes.example.com" {...form.getInputProps('hostsText')} />
        </SettingsFields>
        {v.enabled && v.page_enabled && <Text size="xs" c="dimmed">{t('probe.urls')}: {v.path && <Code>{origin}{v.path.replace(/\/+$/, '')}/</Code>} {v.hostsText.split(/[,\s]+/).filter(Boolean).map((h) => <Code key={h} ml={4}>https://{h}/</Code>)}</Text>}
        <SettingsFields>
          <Select label={t('probe.visibility')} data={[{ value: 'public', label: t('probe.vis.public') }, { value: 'users', label: t('probe.vis.users') }, { value: 'admins', label: t('probe.vis.admins') }]} allowDeselect={false} {...form.getInputProps('visibility')} />
          <Switch label={t('probe.showIP')} {...form.getInputProps('show_ip', { type: 'checkbox' })} />
          <Switch label={t('probe.showGlobe')} {...form.getInputProps('show_globe', { type: 'checkbox' })} />
        </SettingsFields>
        <Switch label={t('branding.probeFollow')} description={t('branding.probeFollowHint')} {...form.getInputProps('follow_site_branding', { type: 'checkbox' })} />
        {(!v.follow_site_branding || showIndependentBrandErrors) && <Stack gap="sm">
          {v.follow_site_branding && <Text size="sm" c="red">{t('branding.independentInvalid')}</Text>}
          <TextInput label={t('probe.pageTitle')} placeholder={t('probe.pageTitleHint')} {...form.getInputProps('title')} />
          <SettingsFields>
            <BrandAssetInput label={t('branding.logoLight')} value={v.logo} onChange={value => form.setFieldValue('logo', value)} />
            <BrandAssetInput label={t('branding.logoDark')} value={v.logo_dark ?? ''} onChange={value => form.setFieldValue('logo_dark', value)} dark />
            <BrandAssetInput label={t('branding.favicon')} value={v.favicon ?? ''} onChange={value => form.setFieldValue('favicon', value)} />
          </SettingsFields>
          <Text size="xs" c="dimmed">{t('branding.uploadHint')}</Text>
          <Text size="xs" c="dimmed">{t('branding.fallbackHint')}</Text>
        </Stack>}
        <MultiSelect label={t('monitoring.publicSections')} description={t('monitoring.publicHint')} data={['cpu', 'memory', 'disk', 'network', 'system', 'info', 'traffic', 'latency', 'history', 'availability'].map((value) => ({ value, label: t(`monitoring.sections.${value}`) }))} {...form.getInputProps('public_sections')} />
        <SettingsFields>
          <Select label={t('monitoring.layout')} data={['grid', 'compact'].map((value) => ({ value, label: t(`monitoring.layouts.${value}`) }))} allowDeselect={false} {...form.getInputProps('layout')} />
          <Select label={t('probe.financeCurrency')} description={t('probe.financeCurrencyHint')} searchable allowDeselect={false} data={currencyOptions(i18n.language, v.finance_currency)} {...form.getInputProps('finance_currency')} />
        </SettingsFields>
        <ProbeAppearanceFields value={v.appearance} onChange={(a) => form.setFieldValue('appearance', a)} />
        <Text size="sm" fw={600} mt="xs">{t('probe.alerts')}</Text>
        <SettingsFields>
          <NumberInput label={t('probe.offline')} min={30} {...form.getInputProps('alerts.offline_seconds')} />
          <NumberInput label="CPU %" min={0} max={100} {...form.getInputProps('alerts.cpu_pct')} />
          <NumberInput label={t('probe.memPct')} min={0} max={100} {...form.getInputProps('alerts.mem_pct')} />
          <NumberInput label={t('probe.diskPct')} min={0} max={100} {...form.getInputProps('alerts.disk_pct')} />
          <NumberInput label={t('probe.window')} min={1} {...form.getInputProps('alerts.window_minutes')} />
          <Switch label={t('probe.trafficAlert')} mb={6} {...form.getInputProps('alerts.traffic', { type: 'checkbox' })} />
        </SettingsFields>
        <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></form>

    </Card>
  )
}

export function PingTasks() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['ping-tasks'], queryFn: () => api.get<PingTask[]>('/api/admin/ping-tasks') })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const [adding, setAdding] = useState(false)
  const form = useForm({ initialValues: { Name: '', Type: 'tcp', Target: '', IntervalSeconds: 30, NodeIDs: [] as string[] } })
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.post('/api/admin/ping-tasks', { name: v.Name, type: v.Type, target: v.Target, interval_seconds: v.IntervalSeconds, node_ids: v.NodeIDs.map(Number), enabled: true }), onSuccess: () => { form.resetDirty(); toast.ok(t('common.saved')); form.reset(); setAdding(false); qc.invalidateQueries({ queryKey: ['ping-tasks'] }) }, onError: toast.err })
  const toggle = useMutation({ mutationFn: (tk: PingTask) => api.patch(`/api/admin/ping-tasks/${tk.id}`, { ...tk, node_ids: tk.node_ids ?? [], enabled: !tk.enabled }), onSuccess: () => qc.invalidateQueries({ queryKey: ['ping-tasks'] }), onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/ping-tasks/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['ping-tasks'] }), onError: toast.err })
  if (q.data === undefined) return <SettingsLoadState query={q} />
  return (
    <Stack gap="xs" mt="lg">
      <Group justify="space-between"><Text size="sm" fw={600}>{t('probe.tasks')}</Text><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => setAdding((a) => !a)}>{t('probe.addTask')}</Button></Group>
      <Text size="xs" c="dimmed">{t('probe.tasksHint')}</Text><Text size="xs" c="dimmed">{t('networkQuality.semantics')}</Text>
      {adding && (
        <form onSubmit={form.onSubmit((v) => save.mutate(v))}><Group align="flex-end" wrap="wrap">
          <TextInput label={t('probe.taskName')} required style={{ flex: 1, minWidth: 120 }} {...form.getInputProps('Name')} />
          <Select label={t('probe.taskType')} data={['icmp', 'tcp', 'http', 'download']} allowDeselect={false} style={{ width: 110 }} {...form.getInputProps('Type')} />
          <TextInput label={t('probe.taskTarget')} placeholder="1.1.1.1 / host:443 / https://…" required style={{ flex: 2, minWidth: 180 }} {...form.getInputProps('Target')} />
          <NumberInput label={t('probe.taskInterval')} min={5} style={{ width: 110 }} {...form.getInputProps('IntervalSeconds')} />
          <MultiSelect label={t('probe.taskNodes')} placeholder={t('common.all')} data={(nodes.data ?? []).map((n) => ({ value: String(n.id), label: n.name }))} style={{ flex: 1, minWidth: 160 }} {...form.getInputProps('NodeIDs')} />
          <Button type="submit" size="xs" mb={2} loading={save.isPending}>{t('common.save')}</Button>
        </Group></form>
      )}
      {(q.data ?? []).length > 0 && (
        <Table fz="sm"><Table.Tbody>
          {q.data!.map((tk) => (
            <Table.Tr key={tk.id}>
              <Table.Td><Text fw={600}>{tk.name}</Text></Table.Td>
              <Table.Td><Code>{tk.type}</Code> {tk.target}</Table.Td>
              <Table.Td>{tk.interval_seconds}s</Table.Td>
              <Table.Td>{(tk.node_ids ?? []).length ? t('probe.taskNodeCount', { count: tk.node_ids!.length }) : t('common.all')}</Table.Td>
              <Table.Td><Switch size="xs" checked={tk.enabled} onChange={() => toggle.mutate(tk)} /></Table.Td>
              <Table.Td><ActionIcon variant="subtle" color="red" onClick={() => del.mutate(tk.id)}><IconTrash size={14} /></ActionIcon></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Stack>
  )
}
