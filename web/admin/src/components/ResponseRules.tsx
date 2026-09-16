import { ActionIcon, Badge, Button, Card, Code, Group, Select, Stack, Switch, Table, Text, TextInput, Textarea, Tooltip } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconArrowDown, IconArrowUp, IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

interface Match { header: string; op: string; value?: string }
interface Rule { name: string; enabled: boolean; match: Match[]; any_of?: boolean; action: string; format?: string; template?: string; headers?: Record<string, string> }
interface TestResult { rule: string | null; action: string; format: string }

const formats = ['clash', 'stash', 'singbox', 'surge', 'surfboard', 'loon', 'qx', 'egern', 'uri', 'wireguard']
const ops = ['contains', 'equals', 'prefix', 'regex', 'exists', 'missing']
const actions = ['serve', 'block', 'not_found', 'unavailable', 'drop']
const headerNames = ['User-Agent', 'x-hwid', 'x-device-os', 'x-ver-os', 'x-device-model', 'Accept', 'Accept-Language', 'client']

// Ordered rules on the subscription request: the first one whose
// conditions hold picks the format / extra headers / template, or refuses
// the request. Below the list, a tester shows which rule a request hits.
export function ResponseRules() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['response-rules'], queryFn: () => api.get<Rule[]>('/api/admin/settings/response-rules') })
  const [rules, setRules] = useState<Rule[] | null>(null)
  useEffect(() => { if (q.data && rules === null) setRules(q.data) }, [q.data, rules])
  const list = rules ?? q.data ?? []
  const dirty = rules !== null && JSON.stringify(rules) !== JSON.stringify(q.data ?? [])
  const save = useMutation({ mutationFn: () => api.put('/api/admin/settings/response-rules', list), onSuccess: () => { toast.ok(t('common.saved')); setRules(null); qc.invalidateQueries({ queryKey: ['response-rules'] }) }, onError: toast.err })
  const [testUA, setTestUA] = useState('Happ/2.4.0 iOS')
  const [testHeaders, setTestHeaders] = useState('x-hwid: 0123456789abcdef\nx-device-os: iOS')
  const [testClient, setTestClient] = useState('')
  const test = useMutation({
    mutationFn: () => {
      const headers: Record<string, string> = { 'User-Agent': testUA }
      for (const line of testHeaders.split('\n')) { const i = line.indexOf(':'); if (i > 0) headers[line.slice(0, i).trim()] = line.slice(i + 1).trim() }
      return api.post<TestResult>('/api/admin/settings/response-rules/test', { rules: list, headers, client: testClient })
    }, onError: toast.err,
  })
  const set = (i: number, patch: Partial<Rule>) => setRules(list.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  const move = (i: number, d: number) => { const a = [...list]; const j = i + d; if (j < 0 || j >= a.length) return; [a[i], a[j]] = [a[j], a[i]]; setRules(a) }
  const add = () => setRules([...list, { name: `rule-${list.length + 1}`, enabled: true, match: [{ header: 'User-Agent', op: 'contains', value: '' }], action: 'serve', format: '' }])
  const headersText = (h?: Record<string, string>) => Object.entries(h ?? {}).map(([k, v]) => `${k}: ${v}`).join('\n')
  const parseHeaders = (s: string) => { const out: Record<string, string> = {}; for (const line of s.split('\n')) { const i = line.indexOf(':'); if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim() } return out }
  const opNeedsValue = (op: string) => op !== 'exists' && op !== 'missing'
  return (
    <Stack gap="md">
      <Card>
        <Group justify="space-between" mb="xs">
          <div><Text fw={600}>{t('responseRules.title')}</Text><Text size="xs" c="dimmed">{t('responseRules.hint')}</Text></div>
          <Group gap="xs"><Button size="xs" variant="default" leftSection={<IconPlus size={14} />} onClick={add}>{t('responseRules.add')}</Button><Button size="xs" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
        </Group>
        {list.length === 0 && <Text size="sm" c="dimmed">{t('responseRules.empty')}</Text>}
        <Stack gap="sm">
          {list.map((r, i) => (
            <Card key={i} withBorder p="sm">
              <Group justify="space-between" wrap="nowrap" align="flex-start">
                <Group gap="xs" wrap="nowrap" align="flex-end" style={{ flex: 1 }}>
                  <Text size="sm" c="dimmed" w={24}>#{i + 1}</Text>
                  <TextInput size="xs" label={t('responseRules.name')} value={r.name} onChange={(e) => set(i, { name: e.currentTarget.value })} w={160} />
                  <Select size="xs" label={t('responseRules.action')} data={actions.map((a) => ({ value: a, label: t(`responseRules.actions.${a}`) }))} value={r.action} onChange={(v) => v && set(i, { action: v })} allowDeselect={false} w={190} />
                  {r.action === 'serve' && <Select size="xs" label={t('responseRules.format')} data={[{ value: '', label: t('responseRules.formatAuto') }, ...formats.map((f) => ({ value: f, label: f }))]} value={r.format ?? ''} onChange={(v) => set(i, { format: v ?? '' })} allowDeselect={false} w={150} />}
                  <Switch size="xs" label={t('responseRules.enabled')} checked={r.enabled} onChange={(e) => set(i, { enabled: e.currentTarget.checked })} pb={6} />
                </Group>
                <Group gap={2} wrap="nowrap">
                  <ActionIcon size="sm" variant="subtle" onClick={() => move(i, -1)} disabled={i === 0}><IconArrowUp size={14} /></ActionIcon>
                  <ActionIcon size="sm" variant="subtle" onClick={() => move(i, 1)} disabled={i === list.length - 1}><IconArrowDown size={14} /></ActionIcon>
                  <ActionIcon size="sm" variant="subtle" color="red" onClick={() => setRules(list.filter((_, j) => j !== i))}><IconTrash size={14} /></ActionIcon>
                </Group>
              </Group>
              <Group gap="xs" mt="xs" align="center">
                <Text size="xs" c="dimmed">{t('responseRules.when')}</Text>
                <Select size="xs" data={[{ value: 'all', label: t('responseRules.all') }, { value: 'any', label: t('responseRules.any') }]} value={r.any_of ? 'any' : 'all'} onChange={(v) => set(i, { any_of: v === 'any' })} allowDeselect={false} w={150} />
                <Button size="compact-xs" variant="subtle" leftSection={<IconPlus size={12} />} onClick={() => set(i, { match: [...r.match, { header: 'User-Agent', op: 'contains', value: '' }] })}>{t('responseRules.addCondition')}</Button>
              </Group>
              <Table fz="xs" mt={4}><Table.Tbody>
                {r.match.map((m, k) => (
                  <Table.Tr key={k}>
                    <Table.Td w={220}><Select size="xs" searchable data={headerNames.includes(m.header) ? headerNames : [m.header, ...headerNames]} value={m.header} onChange={(v) => v && set(i, { match: r.match.map((x, l) => (l === k ? { ...x, header: v } : x)) })} allowDeselect={false} /></Table.Td>
                    <Table.Td w={140}><Select size="xs" data={ops.map((o) => ({ value: o, label: t(`responseRules.ops.${o}`) }))} value={m.op} onChange={(v) => v && set(i, { match: r.match.map((x, l) => (l === k ? { ...x, op: v } : x)) })} allowDeselect={false} /></Table.Td>
                    <Table.Td>{opNeedsValue(m.op) && <TextInput size="xs" value={m.value ?? ''} placeholder={m.op === 'regex' ? '(?i)happ|v2box' : 'Happ'} onChange={(e) => set(i, { match: r.match.map((x, l) => (l === k ? { ...x, value: e.currentTarget.value } : x)) })} />}</Table.Td>
                    <Table.Td w={36}><ActionIcon size="sm" variant="subtle" color="red" onClick={() => set(i, { match: r.match.filter((_, l) => l !== k) })}><IconTrash size={14} /></ActionIcon></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody></Table>
              {r.match.length === 0 && <Text size="xs" c="orange">{t('responseRules.catchAll')}</Text>}
              {r.action === 'serve' && (
                <Group grow mt="xs" align="flex-start">
                  <Textarea size="xs" autosize minRows={1} label={t('responseRules.headers')} description={t('responseRules.headersHint')} value={headersText(r.headers)} onChange={(e) => set(i, { headers: parseHeaders(e.currentTarget.value) })} />
                  <Textarea size="xs" autosize minRows={1} maxRows={8} label={t('responseRules.template')} description={t('responseRules.templateHint')} value={r.template ?? ''} onChange={(e) => set(i, { template: e.currentTarget.value })} />
                </Group>
              )}
            </Card>
          ))}
        </Stack>
      </Card>
      <Card>
        <Text fw={600} mb="xs">{t('responseRules.test')}</Text>
        <Group align="flex-start" grow>
          <Stack gap="xs">
            <TextInput size="xs" label="User-Agent" value={testUA} onChange={(e) => setTestUA(e.currentTarget.value)} />
            <TextInput size="xs" label={t('responseRules.testClient')} value={testClient} onChange={(e) => setTestClient(e.currentTarget.value)} />
          </Stack>
          <Textarea size="xs" autosize minRows={3} label={t('responseRules.testHeaders')} value={testHeaders} onChange={(e) => setTestHeaders(e.currentTarget.value)} />
        </Group>
        <Group mt="xs" gap="md">
          <Button size="xs" variant="default" loading={test.isPending} onClick={() => test.mutate()}>{t('responseRules.run')}</Button>
          {test.data && (
            <Group gap="xs">
              <Text size="sm">{t('responseRules.result')}</Text>
              {test.data.rule ? <Badge color="teal" variant="light">{test.data.rule}</Badge> : <Badge color="gray" variant="light">{t('responseRules.noRule')}</Badge>}
              <Badge variant="light" color={test.data.action === 'serve' ? 'blue' : 'red'}>{t(`responseRules.actions.${test.data.action}`)}</Badge>
              {test.data.format && <Tooltip label={t('responseRules.format')}><Code>{test.data.format}</Code></Tooltip>}
            </Group>
          )}
        </Group>
      </Card>
    </Stack>
  )
}
