import { Accordion, Alert, Badge, Button, FileButton, Group, MultiSelect, NumberInput, Select, Stack, Switch, Text, Textarea, TextInput, Title } from '@mantine/core'
import { IconUpload } from '@tabler/icons-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { SettingsLoadState } from './SettingsLoadState'

type Localized = string | Record<string, string>
interface Field { key: string; name: Localized; type: string; required?: boolean; options?: string; default?: unknown; help?: Localized }
interface Manifest { short: string; name: Localized; version: string; description?: Localized; author?: Localized; configuration: { type: string; data?: Field[] | string } }
interface Theme { digest: string; short: string; version: string; manifest: Manifest; settings: Record<string, unknown>; revision: number; size: number }
interface Options { nodes: { value: string; label: string }[]; pingtasks: { value: string; label: string }[] }
interface Catalog { packages: Theme[]; state: { active: string; revision: number } }
const base = '/api/admin/settings/probe/themes'
function label(value: Localized | undefined, language: string): string {
  if (typeof value === 'string') return value
  return value?.[language] ?? value?.[language.split('-')[0]] ?? value?.en ?? value?.['zh-CN'] ?? Object.values(value ?? {})[0] ?? ''
}

export function ProbeThemes() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const [selection, setSelection] = useState<string | null>(null)
  const q = useQuery({ queryKey: ['probe-themes'], queryFn: () => api.get<Catalog>(base) })
  const refresh = () => qc.invalidateQueries({ queryKey: ['probe-themes'] })
  const upload = useMutation({ mutationFn: async (file: File) => {
    if (file.size > 32 * 1024 * 1024) throw new Error(t('probeThemes.sizeLimit'))
    const res = await fetch(base, { method: 'POST', body: file, credentials: 'same-origin', headers: { 'Content-Type': 'application/zip' } })
    const body = await res.json()
    if (!res.ok) throw new Error(body.error ?? res.statusText)
    return body as Theme
  }, onSuccess: (theme) => { setSelection(theme.digest); refresh(); toast.ok(t('probeThemes.installed')) }, onError: toast.err })
  const activate = useMutation({ mutationFn: (digest: string) => api.put(`${base}/active`, { digest, revision: q.data!.state.revision }), onSuccess: () => { refresh(); toast.ok(t('common.saved')) }, onError: toast.err })
  const remove = useMutation({ mutationFn: (digest: string) => api.del(`${base}/${digest}`), onSuccess: () => { setSelection(null); refresh() }, onError: toast.err })
  if (q.data === undefined) return <SettingsLoadState query={q} />
  const selected = selection ?? q.data.state.active
  const theme = q.data.packages.find(p => p.digest === selected)
  const active = q.data.packages.find(p => p.digest === q.data.state.active)
  const busy = activate.isPending || remove.isPending || upload.isPending
  return <Stack gap="sm" mt="xl" pt="lg" style={{ borderTop: '1px solid var(--mantine-color-default-border)' }}>
    <Group justify="space-between"><Title order={5}>{t('probeThemes.title')}</Title><FileButton onChange={file => file && upload.mutate(file)} accept=".zip,application/zip">{props => <Button {...props} size="xs" variant="light" leftSection={<IconUpload size={14} />} loading={upload.isPending} disabled={busy}>{t('probeThemes.upload')}</Button>}</FileButton></Group>
    <Text size="sm" c="dimmed">{t('probeThemes.hint')}</Text>
    <Alert variant="light" color="blue">{t('probeThemes.isolation')}</Alert>
    <Group gap="xs"><Text size="sm">{t('probeThemes.current')}</Text><Badge variant="light">{active ? `${label(active.manifest.name, i18n.language)} · ${active.version}` : t('probeThemes.builtin')}</Badge></Group>
    <Select label={t('probeThemes.version')} value={selected} allowDeselect={false} searchable onChange={setSelection} data={[{ value: '', label: t('probeThemes.builtin') }, ...q.data.packages.map(p => ({ value: p.digest, label: `${label(p.manifest.name, i18n.language)} · ${p.version}` }))]} />
    {theme && <><Text size="sm">{label(theme.manifest.description, i18n.language)}</Text><Text size="xs" c="dimmed">{label(theme.manifest.author, i18n.language)} · {(theme.size / 1048576).toFixed(1)} MiB · SHA-256 {theme.digest.slice(0, 16)}…</Text><ThemeSettings key={`${theme.digest}:${theme.revision}`} theme={theme} onSaved={refresh} /></>}
    <Group justify="flex-end">{theme && theme.digest !== q.data.state.active && <Button size="xs" color="red" variant="subtle" disabled={busy} onClick={() => remove.mutate(theme.digest)}>{t('probeThemes.remove')}</Button>}<Button size="xs" loading={activate.isPending} disabled={busy || selected === q.data.state.active} onClick={() => activate.mutate(selected)}>{selected ? t('probeThemes.activate') : t('probeThemes.restore')}</Button></Group>
  </Stack>
}

function ThemeSettings({ theme, onSaved }: { theme: Theme; onSaved: () => void }) {
  const { t, i18n } = useTranslation()
  const options = useQuery({ queryKey: ['probe-theme-options'], queryFn: () => api.get<Options>(`${base}/options`) })
  const fields = Array.isArray(theme.manifest.configuration?.data) ? theme.manifest.configuration.data : []
  const initial: Record<string, unknown> = Object.fromEntries(fields.filter(f => f.key && f.default !== undefined).map(f => [f.key, f.default]))
  const [values, setValues] = useState<Record<string, unknown>>({ ...initial, ...theme.settings })
  const [raw, setRaw] = useState(JSON.stringify(theme.settings, null, 2))
  const managed = !theme.manifest.configuration?.type || theme.manifest.configuration.type === 'managed'
  const change = (key: string, value: unknown) => setValues(v => ({ ...v, [key]: value }))
  const save = useMutation({ mutationFn: () => {
    const settings = managed ? values : JSON.parse(raw)
    if (!settings || typeof settings !== 'object' || Array.isArray(settings)) throw new Error(t('probeThemes.objectRequired'))
    return api.put(`${base}/${theme.digest}/settings`, { revision: theme.revision, settings })
  }, onSuccess: () => { onSaved(); toast.ok(t('common.saved')) }, onError: toast.err })
  return <Accordion variant="separated"><Accordion.Item value="settings"><Accordion.Control>{t('probeThemes.settings')}</Accordion.Control><Accordion.Panel><Stack gap="md">
    <Text size="xs" c="dimmed">{t('probeThemes.publicSettings')}</Text>
    {!managed ? <><Alert color="yellow">{t('probeThemes.customSettings')}</Alert><Textarea label="JSON" value={raw} onChange={e => setRaw(e.currentTarget.value)} autosize minRows={8} maxRows={24} styles={{ input: { fontFamily: 'monospace' } }} /></> : fields.length === 0 ? <Text size="sm" c="dimmed">{t('probeThemes.noSettings')}</Text> : fields.map((field, index) => {
      const props = { label: label(field.name, i18n.language) || field.key, description: label(field.help, i18n.language), required: field.required }
      const value = values[field.key]
      if (field.type === 'title') return <Text key={`title-${index}`} fw={600} mt="sm">{props.label}</Text>
      if (field.type === 'textbox') return <Text key={`text-${index}`} size="sm" style={{ whiteSpace: 'pre-wrap' }}>{props.label}</Text>
      if (field.type === 'switch') return <Switch key={field.key} {...props} checked={value === true} onChange={e => change(field.key, e.currentTarget.checked)} />
      if (field.type === 'number') return <NumberInput key={field.key} {...props} value={typeof value === 'number' ? value : ''} onChange={v => change(field.key, typeof v === 'number' ? v : undefined)} />
      if (field.type === 'select') return <Select key={field.key} {...props} value={typeof value === 'string' ? value : null} data={[...new Set((field.options ?? '').split(',').map(s => s.trim()).filter(Boolean))]} onChange={v => change(field.key, v)} />
      if (field.type === 'nodes' || field.type === 'pingtasks') {
        let parsed = value
        if (typeof parsed === 'string') { try { parsed = JSON.parse(parsed) } catch { parsed = [] } }
        const selected = Array.isArray(parsed) ? parsed.map(String) : []
        const choices = options.data?.[field.type] ?? []
        // Preserve unavailable selections in the draft without revealing private labels.
        const data = [...choices, ...selected.filter(v => !choices.some(o => o.value === v)).map(v => ({ value: v, label: v }))]
        return <MultiSelect key={field.key} {...props} data={data} value={selected} searchable disabled={!options.data} error={options.isError ? t('probeThemes.optionsFailed') : undefined} onChange={v => change(field.key, field.type === 'pingtasks' ? v.map(Number) : v)} />
      }
      if (field.type === 'richtext') return <Textarea key={field.key} {...props} value={typeof value === 'string' ? value : JSON.stringify(value ?? [])} onChange={e => change(field.key, e.currentTarget.value)} autosize minRows={3} maxRows={12} />
      if (field.type === 'string') return <TextInput key={field.key} {...props} value={typeof value === 'string' ? value : ''} onChange={e => change(field.key, e.currentTarget.value)} />
      return <Textarea key={field.key} {...props} value={typeof value === 'string' ? value : JSON.stringify(value ?? null)} onChange={e => change(field.key, e.currentTarget.value)} autosize minRows={2} />
    })}
    <Group justify="flex-end"><Button size="xs" variant="light" loading={save.isPending} disabled={fields.some(f => f.type === 'nodes' || f.type === 'pingtasks') && !options.data} onClick={() => save.mutate()}>{t('probeThemes.saveSettings')}</Button></Group>
  </Stack></Accordion.Panel></Accordion.Item></Accordion>
}
