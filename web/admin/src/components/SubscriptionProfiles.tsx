import { ActionIcon, Badge, Button, Card, Checkbox, Code, ColorInput, Group, JsonInput, Modal, NumberInput, Select, SimpleGrid, Stack, Switch, Table, Text, Textarea, TextInput } from '@mantine/core'
import { modals } from '@mantine/modals'
import { IconPencil, IconTrash } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../lib/api'
import { AccountInfoFields } from './settings/AccountInfoCard'
import type { AccountInfoSettings } from '../lib/api'
import { toast } from '../lib/notify'
import type { SubLibrary } from './SubTemplateLibrary'

type Settings = { account_info?: AccountInfoSettings; title?: string; headers?: Record<string, string>; auto_flags?: boolean; info_lines?: string[]; remark_prefix?: string; hwid?: { enabled?: boolean; require?: boolean; device_limit?: number; announce?: string }; page?: { enabled: boolean; title?: string; description?: string; accent?: string } }
type Profile = { id: number; name: string; default: boolean; settings: Settings; templates: Record<string, number>; assigned_users?: number }
export function SubscriptionProfiles() {
 const { t } = useTranslation()
 const qc = useQueryClient()
 const q = useQuery({ queryKey: ['sub-profiles'], queryFn: () => api.get<Profile[]>('/api/admin/settings/sub-profiles') })
 const library = useQuery({ queryKey: ['sub-library'], queryFn: () => api.get<SubLibrary>('/api/admin/settings/sub-library/templates') })
 const [draft, setDraft] = useState<Profile | null>(null)
 const [headers, setHeaders] = useState('{}')
 const [userID, setUserID] = useState<string | number>('')
 const [format, setFormat] = useState('clash')
 const [rendered, setRendered] = useState('')
 const changed = () => { for (const key of ['sub-profiles', 'user-profile-choices', 'user-sub-profile']) qc.invalidateQueries({ queryKey: [key] }) }
 const payload = (): Profile => ({ ...draft!, settings: { ...draft!.settings, headers: JSON.parse(headers) } })
 const save = useMutation({ mutationFn: () => draft!.id ? api.put(`/api/admin/settings/sub-profiles/${draft!.id}`, payload()) : api.post('/api/admin/settings/sub-profiles', payload()), onSuccess: () => { setDraft(null); changed(); toast.ok(t('common.saved')) }, onError: toast.err })
 const preview = useMutation({ mutationFn: () => api.post<{ body: string }>('/api/admin/settings/sub-profiles/preview', { profile: payload(), user_id: Number(userID), format }), onSuccess: r => setRendered(r.body), onError: toast.err })
 const del = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/settings/sub-profiles/${id}`), onSuccess: changed, onError: e => toast.err(e instanceof ApiError && e.status === 409 ? t('subProfiles.inUse') : e) })
 const edit = (p: Profile) => { setDraft(p); setHeaders(JSON.stringify(p.settings.headers ?? {}, null, 2)); setRendered('') }
 const set = (v: Partial<Settings>) => { if (draft) setDraft({ ...draft, settings: { ...draft.settings, ...v } }); setRendered('') }
 const hwid = (v: Partial<NonNullable<Settings['hwid']>>) => set({ hwid: { ...draft?.settings.hwid, ...v } })
 const page = (v: Partial<NonNullable<Settings['page']>>) => set({ page: { enabled: false, ...draft?.settings.page, ...v } })
 const boolOptions = [{ value: '', label: t('subProfiles.inherit') }, { value: 'true', label: t('subProfiles.on') }, { value: 'false', label: t('subProfiles.off') }]
 const boolean = (v: string | null) => v === 'true' ? true : v === 'false' ? false : undefined
 let validHeaders = false
 try { const v: unknown = JSON.parse(headers); validHeaders = !!v && typeof v === 'object' && !Array.isArray(v) && Object.values(v).every(x => typeof x === 'string') } catch { /* keep incomplete input editable */ }
 return <Card><Stack>
  <Text size="sm" c="dimmed">{t('subProfiles.precedence')}</Text>
  <Group justify="flex-end"><Button onClick={() => edit({ id: 0, name: '', default: false, settings: {}, templates: {} })}>{t('subProfiles.newProfile')}</Button></Group>
  <Table.ScrollContainer minWidth={500}><Table><Table.Thead><Table.Tr><Table.Th>{t('subProfiles.name')}</Table.Th><Table.Th>{t('subProfiles.assigned')}</Table.Th><Table.Th /></Table.Tr></Table.Thead><Table.Tbody>{q.data?.map(p => <Table.Tr key={p.id}><Table.Td><Group gap="xs">{p.name}{p.default && <Badge>{t('subProfiles.default')}</Badge>}</Group></Table.Td><Table.Td>{p.assigned_users ?? 0}</Table.Td><Table.Td><Group><ActionIcon variant="subtle" aria-label={t('common.edit')} onClick={() => edit(p)}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" aria-label={t('common.delete')} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: p.name, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, onConfirm: () => del.mutate(p.id) })}><IconTrash size={16} /></ActionIcon></Group></Table.Td></Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>
  <Modal opened={!!draft} onClose={() => setDraft(null)} title={t('subProfiles.profiles')} size="xl">
   {draft && <Stack>
    <TextInput label={t('subProfiles.name')} maxLength={100} value={draft.name} onChange={e => setDraft({ ...draft, name: e.currentTarget.value })} />
    <Checkbox label={t('subProfiles.default')} description={t('subProfiles.defaultHint')} checked={draft.default} onChange={e => setDraft({ ...draft, default: e.currentTarget.checked })} />
    <Text size="xs" c="dimmed">{t('subProfiles.assigned')}: {draft.assigned_users ?? 0} · {t('subProfiles.changesApply')}</Text>
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
     <TextInput label={t('subProfiles.title')} value={draft.settings.title ?? ''} maxLength={200} onChange={e => set({ title: e.currentTarget.value })} />
     <Select label={t('subProfiles.flags')} allowDeselect={false} value={draft.settings.auto_flags?.toString() ?? ''} data={boolOptions} onChange={v => set({ auto_flags: boolean(v) })} />
     <TextInput label={t('subProfiles.prefix')} value={draft.settings.remark_prefix ?? ''} maxLength={200} onChange={e => set({ remark_prefix: e.currentTarget.value })} />
    </SimpleGrid>
    <Checkbox label={t('accountInfo.override')} checked={draft.settings.account_info !== undefined} onChange={e => set({ account_info: e.currentTarget.checked ? { traffic: false, reset: false, expiry: false } : undefined })} />
    {draft.settings.account_info !== undefined && <AccountInfoFields value={draft.settings.account_info} onChange={v => set({ account_info: v })} />}
    <Checkbox label={t('subProfiles.overrideInfo')} checked={draft.settings.info_lines !== undefined} onChange={e => set({ info_lines: e.currentTarget.checked ? [] : undefined })} />
    {draft.settings.info_lines !== undefined && <Textarea label={t('subProfiles.infoLines')} description={t('subProfiles.infoHint')} autosize minRows={2} value={draft.settings.info_lines.join('\n')} onChange={e => set({ info_lines: e.currentTarget.value ? e.currentTarget.value.split('\n') : [] })} />}
    <Text fw={600}>HWID</Text>
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
     <Select label={t('subProfiles.hwidEnabled')} value={draft.settings.hwid?.enabled?.toString() ?? ''} data={boolOptions} allowDeselect={false} onChange={v => hwid({ enabled: boolean(v) })} />
     <Select label={t('subProfiles.hwidRequired')} value={draft.settings.hwid?.require?.toString() ?? ''} data={boolOptions} allowDeselect={false} onChange={v => hwid({ require: boolean(v) })} />
     <NumberInput label={t('subProfiles.deviceLimit')} description={t('subProfiles.limitHint')} min={0} max={10000} value={draft.settings.hwid?.device_limit ?? ''} onChange={v => hwid({ device_limit: v === '' ? undefined : Number(v) })} />
     <TextInput label={t('subProfiles.announce')} value={draft.settings.hwid?.announce ?? ''} onChange={e => hwid({ announce: e.currentTarget.value || undefined })} maxLength={2000} />
    </SimpleGrid>
    <Text fw={600}>{t('subProfiles.library')}</Text>
    <SimpleGrid cols={{ base: 1, sm: 2 }}>{Object.keys(library.data?.defaults ?? {}).map(f => <Select key={f} label={f} value={draft.templates[f] ? String(draft.templates[f]) : ''} allowDeselect={false} data={[{ value: '', label: t('subProfiles.inherit') }, ...(library.data?.items ?? []).filter(v => v.format === f).map(v => ({ value: String(v.id), label: v.name }))]} onChange={v => { const templates = { ...draft.templates }; if (v) templates[f] = Number(v); else delete templates[f]; setDraft({ ...draft, templates }); setRendered('') }} />)}</SimpleGrid>
    <JsonInput label={t('subProfiles.headers')} description={t('subProfiles.headersHint')} value={headers} onChange={v => { setHeaders(v); setRendered('') }} formatOnBlur autosize minRows={3} />
    <Switch label={t('subProfiles.page')} checked={!!draft.settings.page?.enabled} onChange={e => page({ enabled: e.currentTarget.checked })} />
    {draft.settings.page?.enabled && <Stack gap="sm"><TextInput label={t('subProfiles.pageTitle')} value={draft.settings.page.title ?? ''} maxLength={200} onChange={e => page({ title: e.currentTarget.value })} /><Textarea label={t('subProfiles.description')} value={draft.settings.page.description ?? ''} maxLength={2000} onChange={e => page({ description: e.currentTarget.value })} /><ColorInput label={t('subProfiles.accent')} format="hex" value={draft.settings.page.accent ?? '#5046e5'} onChange={v => page({ accent: v })} /><Text size="xs" c="dimmed">{t('subProfiles.pageHint')}</Text></Stack>}
    <Group align="flex-start"><NumberInput label={t('subProfiles.previewUser')} min={1} value={userID} onChange={setUserID} style={{ flex: '1 1 130px' }} /><Select label={t('subTemplates.format')} value={format} allowDeselect={false} onChange={v => v && setFormat(v)} data={Object.keys(library.data?.defaults ?? {})} style={{ flex: '1 1 130px' }} /><Button mt={25} variant="default" disabled={!Number(userID) || !draft.name.trim() || !validHeaders} loading={preview.isPending} onClick={() => preview.mutate()}>{t('subProfiles.preview')}</Button></Group>
    {rendered && <Code block style={{ maxHeight: 320, overflow: 'auto' }}>{rendered}</Code>}
    <Group justify="flex-end"><Button loading={save.isPending} disabled={!draft.name.trim() || !validHeaders} onClick={() => save.mutate()}>{t('common.save')}</Button></Group>
   </Stack>}
  </Modal>
 </Stack></Card>
}
