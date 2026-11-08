import { Accordion, Alert, Anchor, Button, Stack, Text } from '@mantine/core'
import { useQueries } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { entryIPLabelKey, type EntryIPVersion } from '../lib/entry-ip-policy'

interface Support {
  formats: { format: string; supported: boolean; reason: string }[]
  template_conflicts: string[]
  templates_checked: boolean
}

export function EntryIPSupport({ host, protocol, policy, variants, disabled }: { host: string; protocol: string; policy: EntryIPVersion; variants: boolean; disabled?: boolean }) {
  const { t } = useTranslation()
  const policies = variants ? ['ipv4', 'ipv6', 'dual'] as const : [policy]
  const queries = useQueries({ queries: policies.map(ip => ({
    queryKey: ['entry-ip-support', host.trim(), protocol, ip],
    queryFn: ({ signal }: { signal: AbortSignal }) => api.get<Support>(`/api/admin/entries/ip-version-support?${new URLSearchParams({ host: host.trim(), protocol, ip_version: ip })}`, signal),
    enabled: !disabled && !!host.trim() && !!protocol && !!ip,
    staleTime: 0,
  })) })
  if (!policy || disabled || !host.trim() || !protocol) return null
  const conflicts = [...new Set(queries.flatMap(q => q.data?.template_conflicts ?? []))]
  return <Stack gap={4}>
    {queries.some(q => q.isPending || q.isFetching) ? <Text size="xs" c="dimmed">{t('entryIP.checking')}</Text> : queries.some(q => q.isError) ? <Alert color="yellow"><Text size="xs">{t('entryIP.checkFailed')}</Text><Button variant="subtle" size="compact-xs" onClick={() => queries.forEach(q => void q.refetch())}>{t('entryFlow.retry')}</Button></Alert> : <>
      {!!conflicts.length && <Alert color="yellow"><Text size="xs">{t('entryIP.templateConflict', { formats: conflicts.join(', ') })}</Text><Anchor component={Link} to="/sub-templates" size="xs">{t('workspace.templateEditor')}</Anchor></Alert>}
      <Accordion variant="contained"><Accordion.Item value="compatibility"><Accordion.Control><Text size="xs">{t('entryIP.supportTitle')}</Text></Accordion.Control><Accordion.Panel><Stack gap={4}>
      <Text size="xs" c="dimmed">{t('entryIP.compatibility')}</Text>
      {queries.map((query, index) => query.data && <Stack key={policies[index]} gap={2}>
        {variants && <Text size="xs" fw={600}>{t(entryIPLabelKey(policies[index]))}</Text>}
        <Text size="xs" c="dimmed">{t('entryIP.supported', { formats: query.data.formats.filter(f => f.supported).map(f => f.format).join(', ') || '—' })}</Text>
        {query.data.formats.some(f => !f.supported) && <Text size="xs" c="orange">{t('entryIP.excluded', { formats: query.data.formats.filter(f => !f.supported).map(f => f.format).join(', ') })}</Text>}
      </Stack>)}
      <Text size="xs" c="dimmed">{t('entryIP.protocolSupport')}</Text>
      <Text size="xs" c="dimmed">{t(queries.every(q => q.data?.templates_checked) ? 'entryIP.templatesChecked' : 'entryIP.templatesUnchecked')}</Text>
      </Stack></Accordion.Panel></Accordion.Item></Accordion>
    </>}
  </Stack>
}
