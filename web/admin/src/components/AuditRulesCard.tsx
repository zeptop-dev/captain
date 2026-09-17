import { ActionIcon, Badge, Button, Card, Group, NumberInput, Select, Stack, Switch, Table, TagsInput, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'

interface Rule { id?: number; name: string; match: string[]; action: string; enabled: boolean; hits?: number }
interface Hit { id: number; user_id: number; email?: string; node_name?: string; inbound_tag?: string; rule_name?: string; at: string; client_ip: string; host: string; port: number; action: string }
interface Settings { auto_ban_hits: number; window_hours: number; notify_admin: boolean }

// Panel-wide audit rules: every node blocks (or just reports) connections
// that match; hits land in the log below and, optionally, ban the user.
export function AuditRulesCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['audit-rules'], queryFn: () => api.get<Rule[]>('/api/admin/audit-rules') })
  const sq = useQuery({ queryKey: ['audit-settings'], queryFn: () => api.get<Settings>('/api/admin/settings/audit') })
  const hits = useQuery({ queryKey: ['audit-log'], queryFn: () => api.get<Hit[]>('/api/admin/audit-log?limit=50'), refetchInterval: 30000 })
  const [rules, setRules] = useState<Rule[] | null>(null)
  const [st, setSt] = useState<Settings | null>(null)
  useEffect(() => { if (q.data && rules === null) setRules(q.data) }, [q.data, rules])
  const list = rules ?? q.data ?? []
  const settings = st ?? sq.data ?? { auto_ban_hits: 0, window_hours: 24, notify_admin: false }
  const dirty = (rules !== null && JSON.stringify(rules) !== JSON.stringify(q.data ?? [])) || (st !== null && JSON.stringify(st) !== JSON.stringify(sq.data))
  const save = useMutation({
    mutationFn: async () => { await api.put('/api/admin/audit-rules', list); if (st) await api.put('/api/admin/settings/audit', st) },
    onSuccess: () => { toast.ok(t('common.saved')); setRules(null); setSt(null); qc.invalidateQueries({ queryKey: ['audit-rules'] }); qc.invalidateQueries({ queryKey: ['audit-settings'] }) }, onError: toast.err,
  })
  const set = (i: number, patch: Partial<Rule>) => setRules(list.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  return (
    <Card>
      <Group justify="space-between" mb="xs">
        <div><Title order={5}>{t('audit.title')}</Title><Text size="xs" c="dimmed">{t('audit.hint')}</Text></div>
        <Group gap="xs"><Button size="xs" variant="default" leftSection={<IconPlus size={14} />} onClick={() => setRules([...list, { name: `rule-${list.length + 1}`, match: [], action: 'block', enabled: true }])}>{t('audit.add')}</Button><Button size="xs" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
      </Group>
      <Stack gap="xs">
        {list.length === 0 && <Text size="sm" c="dimmed">{t('audit.empty')}</Text>}
        {list.map((r, i) => (
          <Group key={r.id ?? `new-${i}`} align="flex-end" wrap="nowrap" gap="xs">
            <TextInput size="xs" w={150} label={t('audit.name')} value={r.name} onChange={(e) => set(i, { name: e.currentTarget.value })} />
            <TagsInput size="xs" flex={1} label={t('audit.match')} placeholder="domain:example.com, keyword:casino, ip:198.51.100.0/24" value={r.match} onChange={(v) => set(i, { match: v })} splitChars={[',', ' ']} />
            <Select size="xs" w={130} label={t('audit.action')} data={[{ value: 'block', label: t('audit.block') }, { value: 'log', label: t('audit.log') }]} value={r.action} onChange={(v) => v && set(i, { action: v })} allowDeselect={false} />
            <Switch size="xs" label={t('audit.enabled')} checked={r.enabled} onChange={(e) => set(i, { enabled: e.currentTarget.checked })} pb={6} />
            {typeof r.hits === 'number' && <Badge variant="light" color={r.hits ? 'orange' : 'gray'} mb={6}>{t('audit.hits', { n: r.hits })}</Badge>}
            <ActionIcon size="sm" variant="subtle" color="red" mb={4} onClick={() => setRules(list.filter((_, j) => j !== i))}><IconTrash size={14} /></ActionIcon>
          </Group>
        ))}
      </Stack>
      <Text size="xs" c="dimmed" mt="xs">{t('audit.matchHint')}</Text>
      <Group mt="md" align="flex-end" gap="md">
        <NumberInput size="xs" w={180} label={t('audit.autoBan')} description={t('audit.autoBanHint')} min={0} value={settings.auto_ban_hits} onChange={(v) => setSt({ ...settings, auto_ban_hits: Number(v) || 0 })} />
        <NumberInput size="xs" w={140} label={t('audit.window')} min={1} max={720} value={settings.window_hours} onChange={(v) => setSt({ ...settings, window_hours: Number(v) || 24 })} />
        <Switch size="xs" label={t('audit.notify')} checked={settings.notify_admin} onChange={(e) => setSt({ ...settings, notify_admin: e.currentTarget.checked })} pb={6} />
      </Group>
      <Title order={6} mt="md" mb={4}>{t('audit.recent')}</Title>
      {(hits.data ?? []).length === 0 ? <Text size="xs" c="dimmed">{t('audit.noHits')}</Text> : (
        <Table fz="xs"><Table.Tbody>
          {(hits.data ?? []).map((h) => (
            <Table.Tr key={h.id}>
              <Table.Td><Text size="xs" c="dimmed">{when(h.at)}</Text></Table.Td>
              <Table.Td>{h.email || `#${h.user_id}`}</Table.Td>
              <Table.Td><Badge size="xs" variant="light" color={h.action === 'block' ? 'red' : 'yellow'}>{h.rule_name || h.action}</Badge></Table.Td>
              <Table.Td>{h.host}:{h.port}</Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{h.node_name}{h.inbound_tag ? ' · ' + h.inbound_tag : ''} · {h.client_ip}</Text></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Card>
  )
}
