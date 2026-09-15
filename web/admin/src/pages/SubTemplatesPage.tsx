import { Badge, Button, Card, Code, Group, Select, Stack, Text, Textarea } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

interface Data { templates: Record<string, string>; defaults: Record<string, string> }
const labels: Record<string, string> = { clash: 'mihomo / Clash Meta', stash: 'Stash', surge: 'Surge', surfboard: 'Surfboard', loon: 'Loon', qx: 'Quantumult X', egern: 'Egern' }

// One editable template per subscription format. An empty template means
// the built-in default, which is what the editor shows until it is changed.
export default function SubTemplatesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['sub-templates'], queryFn: () => api.get<Data>('/api/admin/settings/sub-templates') })
  const [format, setFormat] = useState('clash')
  const [body, setBody] = useState('')
  const custom = q.data?.templates[format] ?? ''
  const fallback = q.data?.defaults[format] ?? ''
  useEffect(() => { setBody(custom || fallback) }, [format, custom, fallback])
  const save = useMutation({
    mutationFn: (text: string) => api.put('/api/admin/settings/sub-templates', { templates: { ...(q.data?.templates ?? {}), [format]: text === fallback ? '' : text } }),
    onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['sub-templates'] }) }, onError: toast.err,
  })
  const names = Object.keys(q.data?.defaults ?? {})
  const isYaml = format === 'clash' || format === 'stash'
  return (
    <>
      <PageHeader title={t('subTemplates.title')} subtitle={t('subTemplates.subtitle')} />
      <Card>
        <Stack gap="sm">
          <Group align="flex-end">
            <Select label={t('subTemplates.format')} data={names.map((n) => ({ value: n, label: labels[n] ?? n }))} value={format} onChange={(v) => v && setFormat(v)} allowDeselect={false} style={{ minWidth: 220 }} />
            {custom ? <Badge color="teal">{t('subTemplates.customized')}</Badge> : <Badge color="gray">{t('subTemplates.default')}</Badge>}
          </Group>
          <Text size="xs" c="dimmed">
            {isYaml ? t('subTemplates.hintYaml') : t('subTemplates.hintIni')} <Code>{'{{proxies}}'}</Code> <Code>{'{{proxy_names}}'}</Code>
            {(format === 'loon' || format === 'qx') && ' ' + t('subTemplates.hintNodeList')}
          </Text>
          <Textarea value={body} onChange={(e) => setBody(e.currentTarget.value)} autosize minRows={16} maxRows={40} ff="monospace" fz="xs" spellCheck={false} />
          <Group justify="flex-end">
            <Button variant="default" size="xs" disabled={!custom && body === fallback} onClick={() => { setBody(fallback); save.mutate(fallback) }}>{t('subTemplates.reset')}</Button>
            <Button size="xs" loading={save.isPending} onClick={() => save.mutate(body)}>{t('common.save')}</Button>
          </Group>
        </Stack>
      </Card>
    </>
  )
}
