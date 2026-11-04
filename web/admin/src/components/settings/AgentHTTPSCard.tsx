import { Alert, Anchor, Badge, Button, Card, Code, Group, Select, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../../lib/api'
import { agentHTTPSCheckCommand, agentHTTPSDockerCommand, agentHTTPSFresh, agentHTTPSOrigin, suggestedAgentHTTPSURL, type AgentHTTPSSettings, type AgentHTTPSView } from '../../lib/agent-https'
import { useSettingsForm } from '../../lib/settings-draft'
import { toast } from '../../lib/notify'
import { SettingsLoadState } from '../SettingsLoadState'
import { SettingsFields } from '../SettingsFields'
import { Copy } from '../Copy'

interface CertificateChoice { id: number; name: string; names: string[]; not_after: string }
const queryKey = ['agent-https-settings']

export default function AgentHTTPSCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [reloading, setReloading] = useState(false)
  const [clock, setClock] = useState(() => ({ wall: Date.now(), mono: performance.now(), online: navigator.onLine }))
  const settings = useQuery({ queryKey, queryFn: async ({ signal }) => {
    const start = { wall: Date.now(), mono: performance.now() }
    const view = await api.get<AgentHTTPSView>('/api/admin/settings/agent-https', signal)
    return { view, start }
  }, refetchInterval: 5000, retry: false })
  const certificates = useQuery({ queryKey: ['certificates'], queryFn: ({ signal }) => api.get<{ certificates: CertificateChoice[] }>('/api/admin/certificates', signal), retry: false })
  const form = useSettingsForm<AgentHTTPSSettings>({ initialValues: { enabled: false, url: '', listen: ':2096', certificate_id: 0, revision: '' }, validate: {
    url: (value, values) => values.enabled && !agentHTTPSOrigin(value) ? t('agentHTTPS.invalidURL') : null,
    certificate_id: (value, values) => values.enabled && !value ? t('agentHTTPS.chooseCertificate') : null,
  } })
  const view = settings.data?.view
  useEffect(() => { if (view) form.hydrate(view.settings) }, [view]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const update = () => setClock({ wall: Date.now(), mono: performance.now(), online: navigator.onLine })
    const timer = window.setInterval(update, 1000)
    window.addEventListener('online', update); window.addEventListener('offline', update)
    return () => { clearInterval(timer); window.removeEventListener('online', update); window.removeEventListener('offline', update) }
  }, [])
  const save = useMutation({ mutationFn: async (values: AgentHTTPSSettings) => {
    await qc.cancelQueries({ queryKey })
    return api.put<AgentHTTPSView>('/api/admin/settings/agent-https', values)
  }, onSuccess: result => {
    form.setValues(result.settings); form.resetDirty(result.settings)
    qc.setQueryData(queryKey, { view: result, start: { wall: 0, mono: 0 } })
    // A save response is not a new observation of the listener. Refresh status.
    void qc.invalidateQueries({ queryKey }); void qc.invalidateQueries({ queryKey: ['agent-https-choice'] })
    toast.ok(t('common.saved'))
  }, onError: toast.err })
  if (!view) return <SettingsLoadState query={settings} />
  const fresh = !!settings.data && clock.online && !settings.isError && agentHTTPSFresh(settings.data.start.wall, settings.data.start.mono, clock.wall, clock.mono)
  const statusKey = !fresh ? 'unknown' : view.status.state === 'error' ? 'error' : view.ready && view.status.state === 'listening' ? 'listening' : view.settings.enabled ? 'pending' : 'disabled'
  const choices = (certificates.data?.certificates ?? []).map(c => ({ value: String(c.id), label: `${c.name || c.names.join(', ')} · ${c.names.join(', ')}` }))
  if (form.values.certificate_id && !choices.some(c => c.value === String(form.values.certificate_id))) choices.push({ value: String(form.values.certificate_id), label: t('agentHTTPS.missingCertificate', { id: form.values.certificate_id }) })
  const command = view.settings.enabled ? agentHTTPSDockerCommand(view.settings.url) : null
  const checkCommand = view.settings.enabled ? agentHTTPSCheckCommand(view.settings.url) : null
  return <Card><Stack gap="sm">
    <Title order={5}>{t('agentHTTPS.title')}</Title>
    <Text size="sm" c="dimmed">{t('agentHTTPS.hint')}</Text>
    <form onSubmit={form.onSubmit(values => save.mutate(values))}><fieldset disabled={save.isPending || reloading} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack gap="sm">
      <Switch label={t('agentHTTPS.enable')} {...form.getInputProps('enabled', { type: 'checkbox' })} />
      <SettingsFields>
        <TextInput label={t('agentHTTPS.url')} description={t('agentHTTPS.urlHint')} placeholder={suggestedAgentHTTPSURL(view.base_url) || 'https://panel.example.com:2096'} {...form.getInputProps('url')} />
        <Select searchable clearable label={t('agentHTTPS.certificate')} description={t('agentHTTPS.certificateHint')} data={choices} value={form.values.certificate_id ? String(form.values.certificate_id) : null} onChange={value => form.setFieldValue('certificate_id', Number(value) || 0)} error={form.errors.certificate_id} disabled={certificates.isPending} />
      </SettingsFields>
      {certificates.isError && <Alert color="yellow">{t('agentHTTPS.certificateLoadError')} <Button size="xs" variant="subtle" onClick={() => certificates.refetch()}>{t('workspace.retry')}</Button></Alert>}
      <Group justify="space-between" align="flex-start"><Button size="xs" variant="light" onClick={() => { form.setFieldValue('url', suggestedAgentHTTPSURL(view.base_url)); form.setFieldValue('listen', ':2096') }}>{t('agentHTTPS.useSuggested')}</Button><Anchor component={Link} to="/domains?tab=certs" size="sm">{t('agentHTTPS.manageCertificates')}</Anchor></Group>
      <Text size="xs" c="dimmed">{t('agentHTTPS.portsHint')}</Text>
      <details><summary>{t('agentHTTPS.advanced')}</summary><TextInput mt="sm" label={t('agentHTTPS.listen')} description={t('agentHTTPS.listenHint')} placeholder=":2096" {...form.getInputProps('listen')} /></details>
      {save.error instanceof ApiError && save.error.status === 409 && <Alert color="yellow">{t('agentHTTPS.conflict')}<Button display="block" mt="xs" size="xs" variant="light" onClick={async () => { setReloading(true); try { const result = await settings.refetch(); if (result.data && !result.isError) { form.setValues(result.data.view.settings); form.resetDirty(result.data.view.settings); save.reset() } } finally { setReloading(false) } }}>{t('agentHTTPS.reload')}</Button></Alert>}
      <Group justify="flex-end"><Button type="submit" size="xs" loading={save.isPending} disabled={!form.isDirty()}>{t('settingsLayout.saveSection', { section: t('agentHTTPS.title') })}</Button></Group>
    </Stack></fieldset></form>
    <Group justify="space-between"><Text size="sm" fw={600}>{t('agentHTTPS.localStatus')}</Text><Badge color={statusKey === 'listening' ? 'teal' : statusKey === 'error' ? 'red' : 'gray'}>{t(`agentHTTPS.status.${statusKey}`)}</Badge></Group>
    {view.status.active_url && <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{t('agentHTTPS.activeURL')}: {view.status.active_url} ({view.status.active_listen})</Text>}
    {view.status.certificate_not_after && <Text size="sm">{t('agentHTTPS.expires')}: {new Date(view.status.certificate_not_after).toLocaleString()}</Text>}
    {fresh && view.status.last_error && <Alert color="red">{view.status.last_error}</Alert>}
    <Text size="sm" c="dimmed">{t('agentHTTPS.externalUnknown')}</Text>
    <Text size="sm" c="dimmed">{t('agentConnection.futureOnly')}</Text>
    {checkCommand && <Stack gap="xs"><Text size="sm" fw={600}>{t('agentHTTPS.checkTitle')}</Text><Text size="sm" c="dimmed">{t('agentHTTPS.checkHint')}</Text><Group align="flex-start" wrap="nowrap"><Code block style={{ flex: 1, minWidth: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{checkCommand}</Code><Copy value={checkCommand} /></Group></Stack>}
    {view.in_container && command && <Stack gap="xs"><Text size="sm" fw={600}>{t('agentHTTPS.dockerTitle')}</Text><Text size="sm" c="dimmed">{t('agentHTTPS.dockerHint')}</Text><Group align="flex-start" wrap="nowrap"><Code block style={{ flex: 1, minWidth: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{command}</Code><Copy value={command} /></Group></Stack>}
  </Stack></Card>
}
