import { Alert, Anchor, Badge, Box, Button, Card, Checkbox, Group, MultiSelect, Select, SimpleGrid, Stack, Table, Text, Textarea, TextInput, Title } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type Entry } from '../../lib/api'
import { toast } from '../../lib/notify'
import { useSettingsForm } from '../../lib/settings-draft'
import { maintenanceError, maintenanceLocalTime, missingStatusEntryIDs, parseMaintenanceLocalTime, serviceStateColor, serviceStates, type ServiceStatusSettings, type ServiceStatusSnapshot } from '../../lib/service-status'
import { SettingsLoadState } from '../SettingsLoadState'

const path = '/api/admin/settings/service-status'
const queryKey = ['service-status-settings']
const at = (unix: number | null) => unix ? new Date(unix * 1000).toLocaleString() : '—'

export default function ServiceStatusCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey, queryFn: ({ signal }) => api.get<ServiceStatusSettings>(path, signal), refetchOnWindowFocus: false })
  const entries = useQuery({ queryKey: ['entries'], queryFn: ({ signal }) => api.get<Entry[]>('/api/admin/entries', signal) })
  const preview = useQuery({ queryKey: ['service-status-preview'], queryFn: ({ signal }) => api.get<ServiceStatusSnapshot>(`${path}/preview`, signal), refetchInterval: 60_000 })
  const [conflict, setConflict] = useState<'revision' | 'path' | null>(null)
  const form = useSettingsForm<ServiceStatusSettings>({ initialValues: { revision: 0, public_enabled: false, public_entry_ids: [], maintenance: [] } })
  useEffect(() => { if (q.data) form.hydrate(q.data) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({
    mutationFn: (settings: ServiceStatusSettings) => api.put<ServiceStatusSettings>(path, settings),
    onSuccess: settings => {
      form.setValues(settings); form.resetDirty(settings); qc.setQueryData(queryKey, settings)
      qc.invalidateQueries({ queryKey: ['service-status-preview'] }); setConflict(null); toast.ok(t('common.saved'))
    },
    onError: error => { if (error instanceof ApiError && error.status === 409) setConflict(error.message === 'service status path conflicts with server monitoring' ? 'path' : 'revision'); else toast.err(error) },
  })
  const reset = async () => {
    const result = await q.refetch()
    if (result.data && !result.isError) { form.setValues(result.data); form.resetDirty(result.data); setConflict(null) }
  }
  const reload = () => {
    if (!form.isDirty()) { void reset(); return }
    modals.openConfirmModal({ title: t('serviceStatus.reload'), children: <Text size="sm">{t('serviceStatus.reloadHint')}</Text>, labels: { confirm: t('serviceStatus.discardReload'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => { void reset() } })
  }
  if (q.data === undefined) return <SettingsLoadState query={q} />
  if (entries.data === undefined) return <SettingsLoadState query={entries} />
  const missing = missingStatusEntryIDs(form.values, entries.data)
  const choices = [...entries.data.map(entry => ({ value: String(entry.ID), label: `${entry.Name}${entry.Enabled ? '' : ` (${t('common.disabled')})`}` })), ...missing.map(id => ({ value: String(id), label: t('serviceStatus.deletedEntry', { id }) }))]
  const maintenanceErrors = form.values.maintenance.map(maintenanceError)
  const maintenanceIDs = form.values.maintenance.map(row => row.entry_id)
  const duplicates = new Set(maintenanceIDs).size !== maintenanceIDs.length
  const blocked = q.isError || entries.isError || missing.length > 0 || maintenanceErrors.some(Boolean) || duplicates
  const submit = (settings: ServiceStatusSettings) => {
    // Defend the submit path as well as the button: cached data may remain
    // visible after a failed refetch, but cannot authorize an overwrite.
    if (blocked || save.isPending || q.isFetching || conflict === 'revision') return
    save.mutate(settings)
  }
  return <Stack>
    <Box><Title order={4}>{t('settingsLayout.statusTitle')}</Title><Text size="sm" c="dimmed" mt={4}>{t('settingsLayout.statusAudience')}</Text></Box>
    <Alert>{t('serviceStatus.hint')}</Alert>
    {(q.isError || entries.isError) && <Alert color="red"><Stack align="flex-start"><Text size="sm">{t('workspace.loadError')}</Text><Button variant="light" size="xs" onClick={() => { q.refetch(); entries.refetch() }}>{t('workspace.retry')}</Button></Stack></Alert>}
    {conflict && <Alert color="yellow"><Stack align="flex-start"><Text size="sm">{t(conflict === 'path' ? 'serviceStatus.pathConflict' : 'serviceStatus.conflict')}</Text>{conflict === 'revision' && <Button variant="light" size="xs" onClick={reload}>{t('serviceStatus.reload')}</Button>}</Stack></Alert>}
    <form onSubmit={form.onSubmit(submit)}><fieldset disabled={save.isPending || q.isFetching} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack>
      <Card><Stack>
        <Title order={5}>{t('serviceStatus.visibility')}</Title>
        <Text size="sm" c="dimmed">{t('serviceStatus.portalHint')}</Text>
        <Checkbox label={t('serviceStatus.publicEnabled')} {...form.getInputProps('public_enabled', { type: 'checkbox' })} />
        <Text size="sm" c="dimmed">{t('serviceStatus.publicHint')}</Text>
        <MultiSelect label={t('serviceStatus.publicEntries')} description={t('serviceStatus.publicEntriesHint')} searchable clearable data={choices} value={form.values.public_entry_ids.map(String)} onChange={values => form.setFieldValue('public_entry_ids', values.map(Number))} />
        {form.values.public_enabled && form.values.public_entry_ids.length === 0 && <Alert color="yellow">{t('serviceStatus.publicEmpty')}</Alert>}
        <Group><Anchor href="/portal/status" target="_blank" rel="noreferrer">{t('serviceStatus.openPortal')}</Anchor>{q.data.public_enabled && <Anchor href="/service-status" target="_blank" rel="noreferrer">{t('serviceStatus.openPublic')}</Anchor>}</Group>
      </Stack></Card>
      <Card><Stack>
        <Group justify="space-between"><Title order={5}>{t('serviceStatus.maintenanceTitle')}</Title><Button variant="light" size="xs" leftSection={<IconPlus size={15} />} onClick={() => { const now = Math.floor(Date.now() / 1000); form.insertListItem('maintenance', { entry_id: 0, starts_at: now, ends_at: now + 3600, note: '' }) }}>{t('serviceStatus.addMaintenance')}</Button></Group>
        <Text size="sm" c="dimmed">{t('serviceStatus.maintenanceHint')}</Text>
        {form.values.maintenance.length === 0 && <Text c="dimmed" size="sm">{t('serviceStatus.noMaintenance')}</Text>}
        {form.values.maintenance.map((row, index) => <Card key={form.key(`maintenance.${index}`)} withBorder><Stack gap="sm">
          <Select required searchable label={t('serviceStatus.entry')} data={choices.filter(choice => Number(choice.value) === row.entry_id || !maintenanceIDs.includes(Number(choice.value)))} value={row.entry_id ? String(row.entry_id) : null} onChange={value => form.setFieldValue(`maintenance.${index}.entry_id`, Number(value))} />
          <SimpleGrid cols={{ base: 1, sm: 2 }}><TextInput required type="datetime-local" step={1} label={t('serviceStatus.startsAt')} value={maintenanceLocalTime(row.starts_at)} onChange={event => form.setFieldValue(`maintenance.${index}.starts_at`, parseMaintenanceLocalTime(event.currentTarget.value))} /><TextInput required type="datetime-local" step={1} label={t('serviceStatus.endsAt')} value={maintenanceLocalTime(row.ends_at)} onChange={event => form.setFieldValue(`maintenance.${index}.ends_at`, parseMaintenanceLocalTime(event.currentTarget.value))} /></SimpleGrid>
          <Textarea label={t('serviceStatus.note')} description={t('serviceStatus.noteHint')} autosize minRows={2} maxRows={5} value={row.note} onChange={event => form.setFieldValue(`maintenance.${index}.note`, event.currentTarget.value)} />
          {maintenanceErrors[index] && <Text c="red" size="sm" role="alert">{t(`serviceStatus.validation.${maintenanceErrors[index]}`)}</Text>}
          <Group justify="flex-end"><Button color="red" variant="subtle" size="xs" leftSection={<IconTrash size={15} />} onClick={() => form.removeListItem('maintenance', index)}>{t('common.delete')}</Button></Group>
        </Stack></Card>)}
        {duplicates && <Alert color="red">{t('serviceStatus.duplicateMaintenance')}</Alert>}
      </Stack></Card>
      {missing.length > 0 && <Alert color="yellow">{t('serviceStatus.deletedHint')}</Alert>}
      <Group justify="flex-end"><Button variant="default" onClick={reload}>{t('serviceStatus.reload')}</Button><Button type="submit" loading={save.isPending} disabled={blocked || conflict === 'revision' || !form.isDirty()}>{t('settingsLayout.saveSection', { section: t('settingsLayout.statusTitle') })}</Button></Group>
    </Stack></fieldset></form>
    <Card><Stack>
      <Group justify="space-between"><Title order={5}>{t('serviceStatus.preview')}</Title><Button variant="light" size="xs" leftSection={<IconRefresh size={15} />} loading={preview.isFetching} onClick={() => preview.refetch()}>{t('common.refresh')}</Button></Group>
      <Text size="sm" c="dimmed">{t('serviceStatus.previewHint')}</Text>
      {preview.isError ? <Alert color="red">{t('serviceStatus.previewError')}</Alert> : preview.data === undefined ? <SettingsLoadState query={preview} /> : <>
        <Group gap="xs">{serviceStates.map(state => <Badge key={state} color={serviceStateColor[state]}>{t(`serviceStatus.states.${state}`)} {preview.data.summary[state]}</Badge>)}</Group>
        <Text size="xs" c="dimmed">{t('serviceStatus.updatedAt', { time: at(preview.data.generated_at) })}</Text>
        {preview.data.entries.length === 0 ? <Text c="dimmed">{t('serviceStatus.noEntries')}</Text> : <Table.ScrollContainer minWidth={560}><Table verticalSpacing="sm"><Table.Thead><Table.Tr><Table.Th>{t('serviceStatus.entry')}</Table.Th><Table.Th>{t('serviceStatus.state')}</Table.Th><Table.Th>{t('serviceStatus.basis')}</Table.Th><Table.Th>{t('serviceStatus.observedAt')}</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{preview.data.entries.map(entry => <Table.Tr key={entry.id}>
          <Table.Td><Text size="sm" fw={500}>{entry.name}</Text>{entry.category && <Text size="xs" c="dimmed">{entry.category.name}</Text>}</Table.Td>
          <Table.Td><Badge color={serviceStateColor[entry.state] ?? 'gray'}>{t(`serviceStatus.states.${entry.state}`, { defaultValue: t('serviceStatus.states.unknown') })}</Badge></Table.Td>
          <Table.Td><Text size="sm">{t(`serviceStatus.reasons.${entry.reason}`, { defaultValue: t('serviceStatus.reasonFallback') })}</Text>{entry.maintenance?.note && <Text size="xs" style={{ whiteSpace: 'pre-wrap' }}>{entry.maintenance.note}</Text>}<Text size="xs" c="dimmed">{entry.evidence.map(evidence => t(`serviceStatus.evidence.${evidence}`, { defaultValue: t('serviceStatus.evidenceFallback') })).join(' · ')}</Text><Text size="xs" c="dimmed">{t('serviceStatus.noEndToEnd')}</Text></Table.Td>
          <Table.Td><Text size="xs">{at(entry.observed_at)}</Text></Table.Td>
        </Table.Tr>)}</Table.Tbody></Table></Table.ScrollContainer>}
      </>}
    </Stack></Card>
  </Stack>
}
