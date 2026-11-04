import { Alert, Button, Select, Text, TextInput } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { AgentHTTPSChoice } from '../lib/agent-https'
import { useTranslation } from 'react-i18next'

export function AgentConnectionFields({ agentURL, downloadSource, onURLChange, onSourceChange, active = true }: {
  active?: boolean
  agentURL: string
  downloadSource: string
  onURLChange: (value: string) => void
  onSourceChange: (value: 'github' | 'captain') => void
}) {
  const { t } = useTranslation()
  const managed = useQuery({ queryKey: ['agent-https-choice'], queryFn: ({ signal }) => api.get<AgentHTTPSChoice>('/api/admin/nodes/agent-https', signal), enabled: active, staleTime: 0, refetchInterval: active ? 10_000 : false, retry: false })
  return <>
    {managed.data?.enabled && <>
      <Button size="xs" variant="light" disabled={!managed.data.ready || managed.isError || managed.isFetching} onClick={() => { if (managed.data?.ready) { onURLChange(managed.data.url); onSourceChange('captain') } }}>{t('agentHTTPS.useManaged')}</Button>
      <Text size="xs" c="dimmed">{managed.data.ready ? t('agentHTTPS.selectionHint') : t('agentHTTPS.selectionUnavailable')}</Text>
    </>}
    {managed.isError && <Alert color="yellow">{t('agentHTTPS.selectionLoadError')} <Button size="xs" variant="subtle" onClick={() => managed.refetch()}>{t('workspace.retry')}</Button></Alert>}
    <TextInput label={t('agentConnection.url')} description={t('agentConnection.urlHint')} placeholder="https://agent.example.com:18443" value={agentURL} onChange={e => onURLChange(e.currentTarget.value)} />
    <Select label={t('agentConnection.source')} description={t(downloadSource === 'captain' ? 'agentConnection.captainHint' : 'agentConnection.githubHint')} allowDeselect={false} value={downloadSource} onChange={value => onSourceChange(value === 'captain' ? 'captain' : 'github')} data={[{ value: 'github', label: t('agentConnection.github') }, { value: 'captain', label: t('agentConnection.captain') }]} />
  </>
}
