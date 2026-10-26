import { Accordion, ActionIcon, Alert, Anchor, Badge, Button, Card, Checkbox, Divider, Group, MultiSelect, NumberInput, Progress, Select, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconDownload, IconExternalLink, IconPlus, IconRefresh, IconTrash } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../../lib/api'
import { bytes } from '../../lib/format'
import { toast } from '../../lib/notify'
import { useSettingsForm as useForm } from '../../lib/settings-draft'
import { applyClientSave, availableClientAssets, CLIENT_POLL_LIMIT_MS, clientDraft, clientDraftErrors, clientJobProgress, safeClientExternalURL, selectableClientAssets, type CatalogClient, type ClientAsset, type ClientCatalog, type ClientDownloadJob, type ClientDraft, type LegacyClientItem } from '../../lib/client-catalog'
import { SettingsFields } from '../SettingsFields'
import { SettingsLoadState } from '../SettingsLoadState'

const catalogKey = ['client-catalog']
const catalogURL = '/api/admin/settings/client-catalog'
const legacyPlatforms = ['windows', 'macos', 'ios', 'android', 'linux', 'other']

function assetLabel(asset: ClientAsset): string {
  return [asset.version, asset.arch, asset.package_type, asset.name, asset.size > 0 ? bytes(asset.size) : ''].filter(Boolean).join(' · ')
}

export default function ClientsCard() {
  const { t } = useTranslation()
  const polling = useRef({ job: '', started: 0 })
  const q = useQuery({
    queryKey: catalogKey, queryFn: ({ signal }) => api.get<ClientCatalog>(catalogURL, signal), refetchOnWindowFocus: false,
    refetchInterval: query => {
      const job = query.state.data?.job
      if (job?.state !== 'downloading') return false
      if (polling.current.job !== job.id) polling.current = { job: job.id, started: Date.now() }
      return Date.now() - polling.current.started < CLIENT_POLL_LIMIT_MS ? 2000 : false
    },
  })
  const refresh = () => { polling.current = { job: '', started: 0 }; q.refetch() }
  if (!q.data) return <SettingsLoadState query={q} />
  const catalog = q.data
  return <Stack>
    <Card>
      <Group justify="space-between" gap="sm"><Title order={4}>{t('clientCatalog.builtIn')}</Title><Button variant="light" size="xs" leftSection={<IconRefresh size={14} />} loading={q.isFetching} onClick={refresh}>{t('clientCatalog.refreshStatus')}</Button></Group>
      <Text size="sm" c="dimmed" mt="xs">{t('clientCatalog.hint')}</Text>
      <Text size="xs" c="dimmed" mt="xs">{t('clientCatalog.storage', { used: bytes(catalog.storage.used_bytes), limit: bytes(catalog.storage.max_bytes), max: bytes(catalog.storage.max_asset_bytes) })}</Text>
      <Text size="xs" c="dimmed" mt={4}>{t('clientCatalog.cacheHint')}</Text>
      {q.isError && <Alert color="red" mt="sm">{t('workspace.loadError')}</Alert>}
      {catalog.job && <DownloadProgress job={catalog.job} name={catalog.clients.find(client => client.id === catalog.job?.client_id)?.name ?? ''} />}
      {catalog.job?.state === 'downloading' && <Text size="xs" c="dimmed" mt={4}>{t('clientCatalog.pollHint')}</Text>}
    </Card>
    <Accordion variant="separated" multiple>
      {catalog.clients.map(client => <Accordion.Item key={client.id} value={client.id}>
        <Accordion.Control><Group gap="xs" wrap="wrap"><Text fw={600}>{client.name}</Text><Badge variant="light" color={client.kind === 'github' ? 'blue' : 'gray'}>{t(client.kind === 'github' ? 'clientCatalog.githubRelease' : 'clientCatalog.officialLink')}</Badge>{client.platforms.some(p => p.show_home || p.show_portal) && <Badge color="green">{t('clientCatalog.published')}</Badge>}{client.archived && <Badge color="yellow">{t('clientCatalog.archived')}</Badge>}</Group><Text size="xs" c="dimmed" mt={4}>{client.platforms.map(p => t(`clients.platforms.${p.platform}`)).join(' · ')}</Text></Accordion.Control>
        <Accordion.Panel><ClientEditor client={client} job={catalog.job} blocked={q.isError} /></Accordion.Panel>
      </Accordion.Item>)}
    </Accordion>
    <LegacyClientsEditor />
  </Stack>
}

function DownloadProgress({ job, name }: { job: ClientDownloadJob; name: string }) {
  const { t } = useTranslation()
  const progress = clientJobProgress(job)
  return <Alert color={job.state === 'failed' ? 'red' : job.state === 'complete' ? 'green' : 'blue'} mt="sm" role="status">
    <Text fw={500} size="sm">{name} · {t(`clientCatalog.job.${job.state}`)}</Text>
    {job.state === 'downloading' && <><Progress mt="xs" value={progress ?? 100} animated={progress === null} aria-label={t('clientCatalog.downloadProgress')} /><Text size="xs" mt={4}>{bytes(job.received_bytes)}{job.total_bytes > 0 ? ` / ${bytes(job.total_bytes)}` : ''}</Text></>}
    {job.state === 'complete' && <Text size="xs">{t('clientCatalog.downloadCompleteHint')}</Text>}
    {job.error && <Text size="xs" style={{ overflowWrap: 'anywhere' }}>{job.error}</Text>}
  </Alert>
}

function ClientEditor({ client, job, blocked }: { client: CatalogClient; job: ClientDownloadJob | null; blocked: boolean }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const [conflict, setConflict] = useState(false)
  const [downloadID, setDownloadID] = useState<string | null>(null)
  const form = useForm<ClientDraft>({ initialValues: clientDraft(client), validate: values => Object.fromEntries(Object.entries(clientDraftErrors(client, values)).map(([key, error]) => [key, t(`clientCatalog.${error}`)])) })
  useEffect(() => { form.hydrate(clientDraft(client)) }, [client]) // eslint-disable-line react-hooks/exhaustive-deps
  const updateClient = (value: CatalogClient) => {
    qc.setQueryData<ClientCatalog>(catalogKey, previous => previous ? { ...previous, clients: previous.clients.map(item => item.id === value.id ? value : item) } : previous)
  }
  const save = useMutation({
    mutationFn: (value: ClientDraft) => api.put<CatalogClient>(`${catalogURL}/${client.id}`, value),
    onSuccess: (result, submitted) => {
      const merged = applyClientSave(form.getValues(), submitted, result)
      form.setValues(merged.current); form.resetDirty(merged.baseline); setConflict(false)
      updateClient(result); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: catalogKey })
    },
    onError: error => { if (error instanceof ApiError && error.status === 409) setConflict(true); else toast.err(error) },
  })
  const check = useMutation({ mutationFn: () => api.post<CatalogClient>(`${catalogURL}/${client.id}/check`), onSuccess: value => { updateClient(value); if (!value.check_error) toast.ok(t('clientCatalog.checked')) }, onError: error => { toast.err(error); qc.invalidateQueries({ queryKey: catalogKey }) } })
  const download = useMutation({ mutationFn: (assetID: number) => api.post<ClientDownloadJob>(`${catalogURL}/${client.id}/download`, { asset_id: assetID }), onSuccess: value => { qc.setQueryData<ClientCatalog>(catalogKey, previous => previous ? { ...previous, job: value } : previous); qc.invalidateQueries({ queryKey: catalogKey }) }, onError: toast.err })
  const remove = useMutation({ mutationFn: (assetID: number) => api.del(`${catalogURL}/${client.id}/cache/${assetID}`), onSuccess: () => { toast.ok(t('clientCatalog.removed')); qc.invalidateQueries({ queryKey: catalogKey }) }, onError: toast.err })
  const reloadSettings = useMutation({ mutationFn: () => api.get<ClientCatalog>(catalogURL), onSuccess: fresh => {
    const next = fresh.clients.find(item => item.id === client.id)
    if (!next) { toast.err(new Error(t('workspace.loadError'))); return }
    const values = clientDraft(next); form.setValues(values); form.resetDirty(values); setConflict(false); qc.setQueryData(catalogKey, fresh)
  }, onError: toast.err })
  const reload = () => modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('clientCatalog.reloadHint')}</Text>, labels: { confirm: t('clientCatalog.reload'), cancel: t('workspace.stay') }, onConfirm: () => reloadSettings.mutate() })
  const date = (value?: string) => value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(i18n.language) : t('clientCatalog.notChecked')
  const releaseURL = safeClientExternalURL(client.release?.url)
  const homepageURL = safeClientExternalURL(client.homepage)
  const downloadAssets = availableClientAssets(client).filter(file => !client.cached_assets.some(cached => cached.id === file.id))
  const selectedDownload = downloadAssets.find(file => String(file.id) === downloadID)
  const allAssets = availableClientAssets(client)
  return <Stack gap="md">
    <Group gap="sm">{homepageURL && <Anchor href={homepageURL} target="_blank" rel="noopener noreferrer" size="sm">{t('clientCatalog.website')} <IconExternalLink size={12} /></Anchor>}<Badge color={client.format === 'uri' ? 'yellow' : 'blue'} variant="light">{t(client.format === 'uri' ? 'clientCatalog.nodesOnly' : 'clientCatalog.fullConfig')}</Badge></Group>
    <Text size="xs" c="dimmed">{t(client.format === 'uri' ? 'clientCatalog.nodesOnlyHint' : 'clientCatalog.fullConfigHint', { format: client.format })}</Text>
    {client.kind === 'github' ? <Card withBorder padding="sm">
      <Group justify="space-between" align="flex-start"><Stack gap={4}><Text size="sm" fw={500}>{t('clientCatalog.upstream')}: {client.release?.version ?? t('clientCatalog.unknownVersion')}</Text><Text size="xs" c="dimmed">{t('clientCatalog.lastChecked')}: {date(client.checked_at)}</Text>{client.release && <Text size="xs" c="dimmed">{t('clientCatalog.releasedAt')}: {date(client.release.published_at)}</Text>}{releaseURL && <Anchor href={releaseURL} target="_blank" rel="noopener noreferrer" size="xs">{t('clientCatalog.releaseNotes')}</Anchor>}</Stack><Button size="xs" variant="light" leftSection={<IconRefresh size={14} />} loading={check.isPending} onClick={() => check.mutate()}>{t('clientCatalog.check')}</Button></Group>
      {client.check_error && <Alert color="yellow" mt="sm"><Text size="sm">{t('clientCatalog.checkFailed')}</Text><Text size="xs" style={{ overflowWrap: 'anywhere' }}>{client.check_error}</Text></Alert>}
      {(client.release?.skipped_installers ?? 0) > 0 && <Text size="xs" c="dimmed" mt="xs">{t('clientCatalog.skippedInstallers', { count: client.release?.skipped_installers })}</Text>}
      {client.archived && <Text size="sm" c="yellow" mt="sm">{t('clientCatalog.archivedHint')}</Text>}
      <Divider my="sm" />
      <Select label={t('clientCatalog.downloadFile')} description={t('clientCatalog.downloadHint')} placeholder={t('clientCatalog.selectFile')} searchable clearable data={downloadAssets.map(file => ({ value: String(file.id), label: `${t(`clients.platforms.${file.platform}`)} · ${assetLabel(file)}` }))} value={selectedDownload ? downloadID : null} onChange={setDownloadID} />
      <Group justify="flex-end" mt="sm"><Button size="xs" variant="light" leftSection={<IconDownload size={14} />} disabled={!selectedDownload || job?.state === 'downloading'} loading={download.isPending} onClick={() => selectedDownload && download.mutate(selectedDownload.id)}>{t('clientCatalog.download')}</Button></Group>
    </Card> : <Text size="sm" c="dimmed">{t('clientCatalog.storeVersionHint')}</Text>}
    {client.cached_assets.length > 0 && <Stack gap="xs"><Title order={6}>{t('clientCatalog.cached')}</Title>{client.cached_assets.map(asset => {
      const inUse = client.platforms.some(platform => platform.source === 'cache' && (platform.show_home || platform.show_portal) && platform.asset_ids.includes(asset.id))
      return <Card key={asset.id} withBorder padding="sm"><Group justify="space-between" align="flex-start" wrap="nowrap"><Stack gap={3} style={{ minWidth: 0 }}><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{assetLabel(asset)}</Text><Text size="xs" c="dimmed">{date(asset.cached_at)} · {t(asset.verified_by === 'upstream_digest' ? 'clientCatalog.upstreamVerified' : 'clientCatalog.localHash')}</Text><Text size="xs" c="dimmed" ff="monospace" style={{ overflowWrap: 'anywhere' }}>SHA-256: {asset.sha256}</Text>{inUse && <Text size="xs" c="dimmed">{t('clientCatalog.inUse')}</Text>}</Stack><ActionIcon variant="subtle" color="red" aria-label={t('clientCatalog.removeCache', { name: asset.name })} disabled={inUse || job?.state === 'downloading'} loading={remove.isPending && remove.variables === asset.id} onClick={() => modals.openConfirmModal({ title: t('clientCatalog.removeCacheTitle'), children: <Text size="sm">{t('clientCatalog.removeCacheHint', { name: asset.name })}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => remove.mutate(asset.id) })}><IconTrash size={16} /></ActionIcon></Group></Card>
    })}</Stack>}
    <form onSubmit={form.onSubmit(value => { if (!blocked && !conflict && !save.isPending) save.mutate(structuredClone(value)) })}>
      <Stack gap="sm"><Title order={6}>{t('clientCatalog.publication')}</Title><Text size="xs" c="dimmed">{t('clientCatalog.publicationHint')}</Text>
        {conflict && <Alert color="yellow"><Text size="sm">{t('clientCatalog.conflict')}</Text><Button size="xs" variant="light" mt="xs" onClick={reload}>{t('clientCatalog.reload')}</Button></Alert>}
        {form.values.platforms.map((platform, index) => {
          const sourceChoices = client.kind === 'github' ? [{ value: 'official', label: t('clientCatalog.source.official') }, { value: 'cache', label: t('clientCatalog.source.cache') }] : [{ value: platform.platform === 'ios' ? 'store' : 'official', label: t(platform.platform === 'ios' ? 'clientCatalog.source.store' : 'clientCatalog.source.official') }]
          const available = selectableClientAssets(client, platform)
          const options = available.map(file => ({ value: String(file.id), label: assetLabel(file) }))
          for (const id of platform.asset_ids) if (!available.some(file => file.id === id)) options.push({ value: String(id), label: allAssets.find(file => file.id === id) ? `${assetLabel(allAssets.find(file => file.id === id)!)} · ${t('clientCatalog.unavailable')}` : t('clientCatalog.retainedAsset', { id }) })
          const published = client.published.find(item => item.platform === platform.platform)
          return <Card key={platform.platform} withBorder padding="sm"><Stack gap="sm"><Group justify="space-between"><Text fw={600}>{t(`clients.platforms.${platform.platform}`)}</Text><Text size="xs" c="dimmed">{t('clientCatalog.currentVersion')}: {published?.version === 'multiple' ? t('clientCatalog.multipleVersions') : published?.version || t('clientCatalog.unknownVersion')}</Text></Group>
            <Group gap="md"><Switch label={t('clientCatalog.showHome')} {...form.getInputProps(`platforms.${index}.show_home`, { type: 'checkbox' })} /><Switch label={t('clientCatalog.showPortal')} {...form.getInputProps(`platforms.${index}.show_portal`, { type: 'checkbox' })} /><Checkbox label={t('clientCatalog.recommended')} {...form.getInputProps(`platforms.${index}.recommended`, { type: 'checkbox' })} /></Group>
            <SettingsFields><Select label={t('clientCatalog.downloadSource')} allowDeselect={false} data={sourceChoices} {...form.getInputProps(`platforms.${index}.source`)} /><NumberInput label={t('clientCatalog.sort')} description={t('clientCatalog.sortHint')} min={0} max={10000} allowDecimal={false} {...form.getInputProps(`platforms.${index}.sort`)} /></SettingsFields>
            {client.kind === 'github' && <MultiSelect label={t('clientCatalog.publishedFiles')} description={t(platform.source === 'cache' ? 'clientCatalog.cacheSelectionHint' : 'clientCatalog.officialSelectionHint')} searchable clearable data={options} value={platform.asset_ids.map(String)} onChange={ids => form.setFieldValue(`platforms.${index}.asset_ids`, ids.map(Number))} error={form.errors[`platforms.${index}.asset_ids`]} />}
            {published && <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>{t('clientCatalog.currentSource')}: {t(`clientCatalog.source.${published.source}`)} · {published.downloads.map(file => file.label).join(' · ')}</Text>}
          </Stack></Card>
        })}
        <Group justify="flex-end"><Button type="submit" size="sm" disabled={blocked || conflict} loading={save.isPending}>{t('clientCatalog.saveClient', { name: client.name })}</Button></Group>
      </Stack>
    </form>
  </Stack>
}

function LegacyClientsEditor() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['clients-settings'], queryFn: () => api.get<{ items: LegacyClientItem[] }>('/api/admin/settings/clients') })
  const form = useForm<{ items: LegacyClientItem[] }>({ initialValues: { items: [] } })
  useEffect(() => { if (q.data) form.hydrate({ items: q.data.items ?? [] }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({ mutationFn: (value: { items: LegacyClientItem[] }) => api.put('/api/admin/settings/clients', value), onSuccess: (_, submitted) => { form.resetDirty(submitted); toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['clients-settings'] }) }, onError: toast.err })
  if (!q.data) return <SettingsLoadState query={q} />
  return <Card><Title order={4}>{t('clientCatalog.custom')}</Title><Text size="sm" c="dimmed" mt="xs" mb="sm">{t('clientCatalog.customHint')}</Text>
    {q.isError && <Alert color="red" mb="sm">{t('workspace.loadError')}</Alert>}
    <form onSubmit={form.onSubmit(value => { if (!q.isError && !save.isPending) save.mutate(structuredClone(value)) })}><Stack gap="xs">
      {form.values.items.map((_, index) => <Card key={index} withBorder padding="sm"><SettingsFields><TextInput label={t('clients.name')} placeholder="Example client" {...form.getInputProps(`items.${index}.name`)} /><Select label={t('clients.platform')} data={legacyPlatforms.map(platform => ({ value: platform, label: t(`clients.platforms.${platform}`) }))} allowDeselect={false} {...form.getInputProps(`items.${index}.platform`)} /><TextInput label="URL" placeholder="https://example.com/download" {...form.getInputProps(`items.${index}.url`)} /><TextInput label={t('clients.note')} {...form.getInputProps(`items.${index}.note`)} /></SettingsFields><Group justify="flex-end" mt="xs"><ActionIcon variant="subtle" color="red" aria-label={t('common.delete')} onClick={() => form.removeListItem('items', index)}><IconTrash size={16} /></ActionIcon></Group></Card>)}
      <Group justify="space-between"><Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => form.insertListItem('items', { name: '', platform: 'windows', url: '', note: '' })}>{t('clients.add')}</Button><Button type="submit" size="sm" disabled={q.isError} loading={save.isPending}>{t('common.save')}</Button></Group>
    </Stack></form>
  </Card>
}
