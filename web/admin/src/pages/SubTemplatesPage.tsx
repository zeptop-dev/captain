import { Badge, Button, Card, Code, Group, SegmentedControl, Select, Stack, Text, Textarea } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { SubDesigner } from '../components/SubDesigner'
import { SubTemplateLibrary } from '../components/SubTemplateLibrary'
import { SubscriptionProfiles } from '../components/SubscriptionProfiles'
import { ResponseRules } from '../components/ResponseRules'
import { useURLChoice } from '../lib/use-url-choice'
import { SettingsDraftBoundary, useSettingsDirty } from '../lib/settings-draft'

interface Data { templates: Record<string, string>; defaults: Record<string, string> }
interface TemplateSave { key: string; format: string; text: string; templates: Record<string, string> }
const labels: Record<string, string> = { clash: 'mihomo / Clash Meta', stash: 'Stash', surge: 'Surge', surfboard: 'Surfboard', loon: 'Loon', qx: 'Quantumult X', egern: 'Egern' }

// One editable template per subscription format. An empty template means
// the built-in default, which is what the editor shows until it is changed.
export default function SubTemplatesPage() {
  const { t } = useTranslation()
  const [mode, setMode] = useURLChoice('mode', ['design', 'text', 'rules', 'library', 'profiles'], 'design')
  const [format, setFormat] = useURLChoice('format', Object.keys(labels), 'clash')
  const editorModes = [{ value: 'design', label: t('subTemplates.modeDesign') }, { value: 'text', label: t('subTemplates.modeText') }]
  return (
    <>
      <PageHeader title={mode === 'profiles' ? t('subProfiles.profiles') : mode === 'rules' ? t('subTemplates.modeRules') : mode === 'library' ? t('subProfiles.library') : t('workspace.templates')} subtitle={t('subTemplates.subtitle')} actions={(mode === 'design' || mode === 'text') && <SegmentedControl size="xs" value={mode} onChange={setMode} data={editorModes} />} />
      {mode === 'library' && <SubTemplateLibrary />}
      {mode === 'profiles' && <SubscriptionProfiles />}
      {mode === 'design' && <SubDesigner />}
      {mode === 'rules' && <ResponseRules />}
      {mode === 'text' && <SettingsDraftBoundary><TextTemplateEditor format={format} onFormatChange={setFormat} /></SettingsDraftBoundary>}
    </>
  )
}

function TextTemplateEditor({ format, onFormatChange }: { format: string; onFormatChange: (format: string) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['sub-templates'], queryFn: () => api.get<Data>('/api/admin/settings/sub-templates') })
  const custom = q.data?.templates[format] ?? ''
  const fallback = q.data?.defaults[format] ?? ''
  // Drafts belong to a format and its saved/default template baseline.
  // A successful save rebases only the draft from the submitted baseline.
  const [draft, setDraft] = useState<{ key: string; text: string } | null>(null)
  const bodyKey = `${format}\u0000${custom}\u0000${fallback}`
  const body = draft?.key === bodyKey ? draft.text : custom || fallback
  const setBody = (text: string) => setDraft({ key: bodyKey, text })
  useSettingsDirty(body !== (custom || fallback))
  const save = useMutation({
    mutationFn: (submitted: TemplateSave) => api.put<Data>('/api/admin/settings/sub-templates', { templates: submitted.templates }),
    onSuccess: (result, submitted) => {
      // Advance the saved baseline without discarding input typed after submission.
      const nextKey = `${submitted.format}\u0000${result.templates[submitted.format] ?? ''}\u0000${result.defaults[submitted.format] ?? ''}`
      setDraft(current => current?.key !== submitted.key ? current : current.text === submitted.text ? null : { key: nextKey, text: current.text })
      qc.setQueryData(['sub-templates'], result)
      toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['sub-templates'] })
    }, onError: toast.err,
  })
  const submit = (text: string) => save.mutate({ key: bodyKey, format, text, templates: { ...(q.data?.templates ?? {}), [format]: text === fallback ? '' : text } })
  const names = Object.keys(q.data?.defaults ?? {})
  const isYaml = format === 'clash' || format === 'stash'
  return (
    <Card>
      <Stack gap="sm">
        <Group align="flex-end">
          <Select label={t('subTemplates.format')} data={names.map((n) => ({ value: n, label: labels[n] ?? n }))} value={format} onChange={(v) => v && onFormatChange(v)} allowDeselect={false} style={{ minWidth: 220 }} />
          {custom ? <Badge color="teal">{t('subTemplates.customized')}</Badge> : <Badge color="gray">{t('subTemplates.default')}</Badge>}
        </Group>
        <Text size="xs" c="dimmed">
          {isYaml ? t('subTemplates.hintYaml') : t('subTemplates.hintIni')} <Code>{'{{proxies}}'}</Code> <Code>{'{{proxy_names}}'}</Code>
          {(format === 'loon' || format === 'qx') && ' ' + t('subTemplates.hintNodeList')}
        </Text>
        <Textarea value={body} onChange={(e) => setBody(e.currentTarget.value)} autosize minRows={16} maxRows={40} ff="monospace" fz="xs" spellCheck={false} />
        <Group justify="flex-end">
          <Button variant="default" size="xs" disabled={!custom && body === fallback} onClick={() => { setBody(fallback); submit(fallback) }}>{t('subTemplates.reset')}</Button>
          <Button size="xs" loading={save.isPending} onClick={() => submit(body)}>{t('common.save')}</Button>
        </Group>
      </Stack>
    </Card>
  )
}
