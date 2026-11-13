import { Alert, Badge, Button, Group, NumberInput, Select, SimpleGrid, Stack, Switch, Table, Text, Title } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'
export interface RouteWatch { expected: Record<string, string>; confirmations: number; notify: boolean }
export interface RouteEvent { id: number; at: number; carrier: string; city: string; family: string; before: string; after: string; expected: string; kind: string; rules: string; confirmations: number }
export function RouteWatchPanel({ nodeID, policy, events }: { nodeID: number; policy: RouteWatch; events: RouteEvent[] }) {
 const { t } = useTranslation(), qc = useQueryClient()
 const [draft, setDraft] = useState<RouteWatch | null>(null), p = draft ?? policy
 useSettingsDirty(draft !== null && JSON.stringify(draft) !== JSON.stringify(policy))
 const save = useMutation({ mutationFn: () => api.put(`/api/admin/settings/nodes/${nodeID}/route-watch`, p), onSuccess: async () => { await qc.invalidateQueries({ queryKey: ['return-routes', nodeID] }); setDraft(null); toast.ok(t('common.saved')) }, onError: toast.err })
 return <Stack gap="sm"><Title order={5}>{t('insights.routeWatch')}</Title><Text size="sm" c="dimmed">{t('insights.routeHint')}</Text><fieldset disabled={save.isPending} style={{ border: 0, padding: 0, margin: 0 }}><Stack gap="sm"><SimpleGrid cols={{ base: 1, sm: 3 }}>{['CT', 'CU', 'CM'].map(c => <Select key={c} label={c} value={p.expected[c] ?? null} clearable placeholder={t('insights.noExpectation')} data={['cn2', 'possible_gia', '163', '9929', 'cug', '4837', 'cmin2', 'cmi', 'cmnet', 'ctgnet', 'mixed', 'observed'].map(value => ({ value, label: t(`returnRoutes.classes.${value}`, { defaultValue: value.toUpperCase() }) }))} onChange={v => { const expected = { ...p.expected }; if (v) expected[c] = v; else delete expected[c]; setDraft({ ...p, expected }) }} />)}</SimpleGrid><Group align="flex-end"><NumberInput label={t('insights.confirmations')} min={1} max={10} allowDecimal={false} value={p.confirmations} onChange={v => setDraft({ ...p, confirmations: Number(v) })} /><Switch label={t('insights.routeNotify')} checked={p.notify} onChange={e => setDraft({ ...p, notify: e.currentTarget.checked })} /><Button variant="light" loading={save.isPending} disabled={!draft} onClick={() => save.mutate()}>{t('common.save')}</Button></Group></Stack></fieldset>
 <Table.ScrollContainer minWidth={640}><Table><Table.Thead><Table.Tr>{['time', 'series', 'change', 'state'].map(k => <Table.Th key={k}>{t(`insights.${k}`)}</Table.Th>)}</Table.Tr></Table.Thead><Table.Tbody>{events.map(e => <Table.Tr key={e.id}><Table.Td>{new Date(e.at * 1000).toLocaleString()}</Table.Td><Table.Td>{e.carrier} · {t(`returnRoutes.cities.${e.city}`)} · {e.family}</Table.Td><Table.Td>{e.before || '—'} → {e.after}<Text size="xs" c="dimmed">{e.rules} · {e.confirmations}</Text></Table.Td><Table.Td><Badge variant="light">{t(`insights.routeEvents.${e.kind}`)}</Badge></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>{events.length === 0 && <Alert color="gray">{t('insights.noEvents')}</Alert>}</Stack>
}
