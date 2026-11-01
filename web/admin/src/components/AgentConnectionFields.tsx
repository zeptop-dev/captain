import { Select, TextInput } from '@mantine/core'
import { useTranslation } from 'react-i18next'

export function AgentConnectionFields({ agentURL, downloadSource, onURLChange, onSourceChange }: {
  agentURL: string
  downloadSource: string
  onURLChange: (value: string) => void
  onSourceChange: (value: 'github' | 'captain') => void
}) {
  const { t } = useTranslation()
  return <>
    <TextInput label={t('agentConnection.url')} description={t('agentConnection.urlHint')} placeholder="https://agent.example.com:18443" value={agentURL} onChange={e => onURLChange(e.currentTarget.value)} />
    <Select label={t('agentConnection.source')} description={t(downloadSource === 'captain' ? 'agentConnection.captainHint' : 'agentConnection.githubHint')} allowDeselect={false} value={downloadSource} onChange={value => onSourceChange(value === 'captain' ? 'captain' : 'github')} data={[{ value: 'github', label: t('agentConnection.github') }, { value: 'captain', label: t('agentConnection.captain') }]} />
  </>
}
