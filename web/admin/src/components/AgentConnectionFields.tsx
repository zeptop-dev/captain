import { Alert, Button, Select, TextInput } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { endpointChoices } from '../lib/agent-connections'
import { evidenceFresh, useEvidenceClock } from '../lib/evidence-clock'
import { useTranslation } from 'react-i18next'
export function AgentConnectionFields({ agentURL, downloadSource, onURLChange, onSourceChange, active = true }: {
 active?: boolean; agentURL: string; downloadSource: string; onURLChange: (value: string) => void; onSourceChange: (value: 'github' | 'captain') => void
}) {
 const { t } = useTranslation(); const clock = useEvidenceClock(); const [custom, setCustom] = useState(false)
 const managed = useQuery({ queryKey: ['agent-https-choice'], queryFn: ({ signal }) => endpointChoices(signal), enabled: active, refetchInterval: active ? 5000 : false, retry: false })
 const entries = managed.data?.entries ?? []
 const known = entries.some(e => e.url === agentURL)
 const value = custom || (agentURL !== '' && !known) ? '__custom__' : agentURL
 return <>
  <Select label={t('agentEntries.mode')} value={value} allowDeselect={false} data={[{ value: '', label: t('agentEntries.default') }, ...entries.map(e => ({ value: e.url, label: `${e.name} · ${e.url}`, disabled: !e.ready || managed.isError || !evidenceFresh(managed.data?.start, clock) })), { value: '__custom__', label: t('agentEntries.custom') }]} onChange={v => { setCustom(v === '__custom__'); if (v !== '__custom__') onURLChange(v || '') }} />
  {value === '__custom__' && <TextInput label={t('agentConnection.url')} placeholder="https://agent.example.com:2096" value={agentURL} onChange={e => onURLChange(e.currentTarget.value)} />}
  {managed.isError && <Alert color="yellow">{t('agentEntries.loadError')} <Button variant="subtle" onClick={() => managed.refetch()}>{t('workspace.retry')}</Button></Alert>}
  <Select label={t('agentConnection.source')} description={t(downloadSource === 'captain' ? 'agentConnection.captainHint' : 'agentConnection.githubHint')} allowDeselect={false} value={downloadSource} onChange={v => onSourceChange(v === 'captain' ? 'captain' : 'github')} data={[{ value: 'github', label: t('agentConnection.github') }, { value: 'captain', label: t('agentConnection.captain') }]} />
 </>
}
