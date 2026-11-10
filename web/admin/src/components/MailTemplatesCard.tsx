import { Button, Card, Group, Select, Stack, Text, TextInput, Textarea, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'
import { SettingsLoadState } from './SettingsLoadState'
import { SettingsFields } from './SettingsFields'

type Template = { subject: string; body: string; button: string }
type Design = { name: string; logo: string; color: string; footer: string; templates: Record<string, Record<string, Template>> | null }
type Catalogue = { design: Design; defaults: Record<string, Record<string, Template>>; variables: string[] }
const kinds = ['register', 'reset', 'expiry', 'traffic', 'notice', 'test']
const languages = [{ value: 'en', label: 'English' }, { value: 'zh-CN', label: '简体中文' }, { value: 'zh-TW', label: '繁體中文' }, { value: 'ja', label: '日本語' }, { value: 'ru', label: 'Русский' }, { value: 'ko', label: '한국어' }]
export function MailTemplatesCard() {
 const { t } = useTranslation()
 const qc = useQueryClient()
 const query = useQuery({ queryKey: ['mail-templates'], queryFn: () => api.get<Catalogue>('/api/admin/settings/mail/templates') })
 const [draft, setDraft] = useState<Design | null>(null)
 const [kind, setKind] = useState('register')
 const [language, setLanguage] = useState('en')
 const [to, setTo] = useState('')
 const [shown, setShown] = useState<{ key: string; subject: string; html: string } | null>(null)
 useSettingsDirty(draft !== null)
 const save = useMutation({ mutationFn: (d: Design) => api.put<Catalogue>('/api/admin/settings/mail/templates', d), onSuccess: value => { qc.setQueryData(['mail-templates'], value); setDraft(null); toast.ok(t('common.saved')) }, onError: toast.err })
 const preview = useMutation({ mutationFn: (request: { key: string; design: Design; kind: string; language: string }) => api.post<{ subject: string; html: string }>('/api/admin/settings/mail/preview', request).then(value => ({ ...value, key: request.key })), onSuccess: setShown, onError: toast.err })
 const send = useMutation({ mutationFn: (design: Design) => api.post('/api/admin/settings/mail/test', { design, kind, language, To: to }), onSuccess: () => toast.ok(t('mail.testSent')), onError: toast.err })
 if (query.data === undefined) return <SettingsLoadState query={query} />
 const value = draft ?? query.data.design
 const defaults = query.data.defaults[language][kind]
 const custom = value.templates?.[language]?.[kind]
 const template = { subject: custom?.subject || defaults.subject, body: custom?.body || defaults.body, button: custom?.button || defaults.button }
 const key = JSON.stringify({ value, language, kind })
 const set = (patch: Partial<Design>) => setDraft({ ...value, ...patch })
 const setTemplate = (patch: Partial<Template>) => set({ templates: { ...value.templates, [language]: { ...value.templates?.[language], [kind]: { ...template, ...patch } } } })
 const restore = () => { const templates = { ...value.templates, [language]: { ...value.templates?.[language] } }; delete templates[language][kind]; set({ templates }) }
 const variables = ['site', ...(kind === 'register' || kind === 'reset' ? ['code'] : kind === 'expiry' ? ['expires_at', 'expires_date', 'portal_url', 'plan', 'used', 'total', 'reset_at'] : kind === 'traffic' ? ['used_percent', 'portal_url', 'plan', 'used', 'total', 'reset_at'] : kind === 'notice' ? ['subject', 'body'] : [])]
 return <Card>
  <Title order={5} mb="xs">{t('mailTemplates.title')}</Title>
  <Text size="xs" c="dimmed" mb="sm">{t('mailTemplates.hint')}</Text>
  <Stack gap="sm">
   <fieldset disabled={save.isPending} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack gap="sm">
    <SettingsFields>
     <TextInput label={t('mailTemplates.name')} value={value.name} placeholder="Captain" maxLength={200} onChange={e => set({ name: e.currentTarget.value })} />
     <TextInput label={t('mailTemplates.logo')} value={value.logo} placeholder="https://example.com/logo.png" onChange={e => set({ logo: e.currentTarget.value })} />
     <TextInput label={t('mailTemplates.color')} value={value.color} placeholder="#0ea5e9" maxLength={7} onChange={e => set({ color: e.currentTarget.value })} />
    </SettingsFields>
    <Textarea label={t('mailTemplates.footer')} value={value.footer} maxLength={2000} onChange={e => set({ footer: e.currentTarget.value })} />
    <SettingsFields>
     <Select label={t('mailTemplates.kind')} allowDeselect={false} value={kind} data={kinds.map(value => ({ value, label: t(`mailTemplates.kinds.${value}`) }))} onChange={v => { if (v) setKind(v) }} />
     <Select label={t('mail.language')} allowDeselect={false} value={language} data={languages} onChange={v => { if (v) setLanguage(v) }} />
    </SettingsFields>
    <Text size="xs" c="dimmed">{t('mailTemplates.variables')}: {variables.map(v => `{{${v}}}`).join(' · ')}</Text>
    <TextInput label={t('mailTemplates.subject')} value={template.subject} maxLength={500} onChange={e => setTemplate({ subject: e.currentTarget.value })} />
    <Textarea label={t('mailTemplates.body')} description={t('mailTemplates.bodyHint')} autosize minRows={4} value={template.body} maxLength={12000} onChange={e => setTemplate({ body: e.currentTarget.value })} />
    {(kind === 'expiry' || kind === 'traffic') && <TextInput label={t('mailTemplates.button')} value={template.button} maxLength={100} onChange={e => setTemplate({ button: e.currentTarget.value })} />}
    <Group justify="space-between"><Button variant="subtle" onClick={restore}>{t('mailTemplates.restore')}</Button><Button loading={save.isPending} disabled={draft === null} onClick={() => save.mutate(value)}>{t('settingsLayout.saveSection', { section: t('mailTemplates.title') })}</Button></Group>
   </Stack></fieldset>
   <Text size="xs" c="dimmed">{t('mailTemplates.previewHint')}</Text>
   <Group align="flex-end"><Button variant="light" loading={preview.isPending} onClick={() => preview.mutate({ key, design: value, kind, language })}>{t('mailTemplates.preview')}</Button><TextInput label={t('mail.testTo')} placeholder="you@example.com" value={to} onChange={e => setTo(e.currentTarget.value)} /><Button variant="default" loading={send.isPending} disabled={!to.includes('@')} onClick={() => send.mutate(value)}>{t('mailTemplates.send')}</Button></Group>
   {shown?.key === key && <><Text fw={600} style={{ overflowWrap: 'anywhere' }}>{shown.subject}</Text><iframe title={t('mailTemplates.preview')} sandbox="" referrerPolicy="no-referrer" srcDoc={`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src https:">${shown.html}`} style={{ width: '100%', height: 480, border: '1px solid var(--mantine-color-default-border)', borderRadius: 8 }} /></>}
  </Stack>
 </Card>
}
