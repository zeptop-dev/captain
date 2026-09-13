import { ActionIcon, Button, Card, Code, Group, MultiSelect, NumberInput, Select, Stack, Switch, Table, Text, TextInput, Title, Textarea } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Node } from '../lib/api'
import { toast } from '../lib/notify'

interface ProbeSettings {
  enabled: boolean; beat_seconds: number; carrier_ping: boolean; carriers?: { name: string; addr: string }[]; path: string; hosts: string[]; visibility: string; title: string; logo: string; show_globe: boolean; show_ip: boolean
  alerts: { offline_seconds: number; cpu_pct: number; mem_pct: number; disk_pct: number; window_minutes: number; traffic: boolean }
}
interface PingTask { id: number; name: string; type: string; target: string; interval_seconds: number; node_ids: number[] | null; enabled: boolean }

// Status page (probe) settings: master switch, address (path and/or
// dedicated hosts), visibility, appearance, alert thresholds, ping tasks.
export function ProbeCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['probe-settings'], queryFn: () => api.get<ProbeSettings>('/api/admin/settings/probe') })
  const form = useForm<ProbeSettings & { hostsText: string; carriersText: string }>({ initialValues: { enabled: false, beat_seconds: 10, carrier_ping: true, carriersText: '', path: '/status', hosts: [], hostsText: '', visibility: 'public', title: '', logo: '', show_globe: false, show_ip: false, alerts: { offline_seconds: 180, cpu_pct: 0, mem_pct: 0, disk_pct: 0, window_minutes: 5, traffic: true } } })
  useEffect(() => { if (q.data) form.setValues({ ...q.data, hostsText: (q.data.hosts ?? []).join(', '), carriersText: (q.data.carriers ?? []).map((c) => `${c.name} ${c.addr}`).join('\n') }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const parseCarriers = (text: string) => text.split('\n').map((l) => l.trim()).filter(Boolean).map((l) => { const [name, ...rest] = l.split(/[\s,=]+/); return { name, addr: rest.join('') } })
  const save = useMutation({ mutationFn: (v: ProbeSettings & { hostsText: string; carriersText: string }) => api.put('/api/admin/settings/probe', { ...v, hosts: v.hostsText.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean), carriers: parseCarriers(v.carriersText) }), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['probe-settings'] }) }, onError: toast.err })
  const v = form.values
  const origin = window.location.origin
  return (
    <Card>
      <Title order={5} mb="xs">{t('probe.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('probe.hint')}</Text>
      <form onSubmit={form.onSubmit((vals) => save.mutate(vals))}><Stack gap="sm">
        <Group grow align="flex-end">
          <Switch label={t('probe.enabled')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
          <NumberInput label={t('probe.beat')} min={3} max={300} {...form.getInputProps('beat_seconds')} />
          <Switch label={t('probe.carrier')} {...form.getInputProps('carrier_ping', { type: 'checkbox' })} />
        </Group>
        {v.carrier_ping && <Textarea label={t('probe.carriers')} description={t('probe.carriersHint')} autosize minRows={2} placeholder={'CT ct.tz.cloudcpp.com:80\nCU cu.tz.cloudcpp.com:80\nCM cm.tz.cloudcpp.com:80'} styles={{ input: { fontFamily: 'monospace', fontSize: 12 } }} {...form.getInputProps('carriersText')} />}
        <Group grow align="flex-end">
          <TextInput label={t('probe.path')} description={t('probe.pathHint')} placeholder="/status" {...form.getInputProps('path')} />
          <TextInput label={t('probe.hosts')} description={t('probe.hostsHint')} placeholder="status.example.com, probes.example.com" {...form.getInputProps('hostsText')} />
        </Group>
        {v.enabled && <Text size="xs" c="dimmed">{t('probe.urls')}: {v.path && <Code>{origin}{v.path.replace(/\/+$/, '')}/</Code>} {v.hostsText.split(/[,\s]+/).filter(Boolean).map((h) => <Code key={h} ml={4}>https://{h}/</Code>)}</Text>}
        <Group grow align="flex-end">
          <Select label={t('probe.visibility')} data={[{ value: 'public', label: t('probe.vis.public') }, { value: 'users', label: t('probe.vis.users') }, { value: 'admins', label: t('probe.vis.admins') }]} allowDeselect={false} {...form.getInputProps('visibility')} />
          <Switch label={t('probe.showIP')} {...form.getInputProps('show_ip', { type: 'checkbox' })} />
          <Switch label={t('probe.showGlobe')} {...form.getInputProps('show_globe', { type: 'checkbox' })} />
        </Group>
        <Group grow>
          <TextInput label={t('probe.pageTitle')} placeholder={t('probe.pageTitleHint')} {...form.getInputProps('title')} />
          <TextInput label={t('probe.logo')} placeholder="https://…/logo.png" {...form.getInputProps('logo')} />
        </Group>
        <Text size="sm" fw={600} mt="xs">{t('probe.alerts')}</Text>
        <Group grow align="flex-end">
          <NumberInput label={t('probe.offline')} min={30} {...form.getInputProps('alerts.offline_seconds')} />
          <NumberInput label="CPU %" min={0} max={100} {...form.getInputProps('alerts.cpu_pct')} />
          <NumberInput label={t('probe.memPct')} min={0} max={100} {...form.getInputProps('alerts.mem_pct')} />
          <NumberInput label={t('probe.diskPct')} min={0} max={100} {...form.getInputProps('alerts.disk_pct')} />
          <NumberInput label={t('probe.window')} min={1} {...form.getInputProps('alerts.window_minutes')} />
          <Switch label={t('probe.trafficAlert')} mb={6} {...form.getInputProps('alerts.traffic', { type: 'checkbox' })} />
        </Group>
        <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></form>
      <PingTasks />
    </Card>
  )
}

function PingTasks() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['ping-tasks'], queryFn: () => api.get<PingTask[]>('/api/admin/ping-tasks') })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes') })
  const [adding, setAdding] = useState(false)
  const form = useForm({ initialValues: { Name: '', Type: 'tcp', Target: '', IntervalSeconds: 30, NodeIDs: [] as string[] } })
  const save = useMutation({ mutationFn: (v: typeof form.values) => api.post('/api/admin/ping-tasks', { name: v.Name, type: v.Type, target: v.Target, interval_seconds: v.IntervalSeconds, node_ids: v.NodeIDs.map(Number), enabled: true }), onSuccess: () => { toast.ok(t('common.saved')); form.reset(); setAdding(false); qc.invalidateQueries({ queryKey: ['ping-tasks'] }) }, onError: toast.err })
  const toggle = useMutation({ mutationFn: (tk: PingTask) => api.patch(`/api/admin/ping-tasks/${tk.id}`, { ...tk, node_ids: tk.node_ids ?? [], enabled: !tk.enabled }), onSuccess: () => qc.invalidateQueries({ queryKey: ['ping-tasks'] }), onError: toast.err })
  const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/ping-tasks/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ['ping-tasks'] }), onError: toast.err })
  return (
    <Stack gap="xs" mt="lg">
      <Group justify="space-between"><Text size="sm" fw={600}>{t('probe.tasks')}</Text><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => setAdding((a) => !a)}>{t('probe.addTask')}</Button></Group>
      <Text size="xs" c="dimmed">{t('probe.tasksHint')}</Text>
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
