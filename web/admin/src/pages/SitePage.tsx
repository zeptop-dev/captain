import { Alert, MultiSelect, Button, Card, Group, JsonInput, Select, Stack, Switch, Text, TextInput, Textarea, Title, Anchor } from '@mantine/core'
import { useSettingsForm as useForm } from '../lib/settings-draft'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type Entry, type SiteSettings } from '../lib/api'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { SettingsLoadState } from '../components/SettingsLoadState'
import { SettingsFields } from '../components/SettingsFields'
import { BrandAssetInput } from '../components/BrandAssetInput'
import { modals } from '@mantine/modals'
import { siteValues, sitePayload, missingMapEntryIDs, type SiteValues } from '../components/site/site-form'
import { publicBrandQueryKey, validBrandAssetInput } from '../lib/branding'

export default function SitePage({ embedded = false }: { embedded?: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [mapConflict, setMapConflict] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [homepageOpen, setHomepageOpen] = useState<boolean | null>(null)
  const [homepageAdvancedOpen, setHomepageAdvancedOpen] = useState(false)
  const entries = useQuery({ queryKey: ['entries'], queryFn: () => api.get<Entry[]>('/api/admin/entries') })
  const q = useQuery({ queryKey: ['site-settings'], queryFn: () => api.get<SiteSettings>('/api/admin/settings/site') })
  const form = useForm<SiteValues>({
    initialValues: { homepage_enabled: true, map_revision: 0, map_source: 'manual', status_map_source: 'manual', map_entry_ids: [], logo_light: '', logo_dark: '', favicon: '', brand_title: '', name: '', tagline: '', description: '', show_plans: true, telegram: '', tos: '', download: '', github: '', hub: '', locations: '[]', features: '[]', faq: '[]', primary: 'cyan', radius: 'lg', scheme: 'light', site_scheme: undefined, font_family: '', portal_title: '', inject_head: '', inject_body: '' },
    validate: { logo_light: v => validBrandAssetInput(v) ? null : t('branding.invalidURL'), logo_dark: v => validBrandAssetInput(v) ? null : t('branding.invalidURL'), favicon: v => validBrandAssetInput(v) ? null : t('branding.invalidURL'), locations: json, features: json, faq: json, hub: (v) => (v.trim() === '' ? null : json(v)) },
  })
  useEffect(() => {
    const s = q.data
    if (!s) return
    form.hydrate(siteValues(s))
  }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({
    mutationFn: (v: SiteValues) => api.put<SiteSettings>('/api/admin/settings/site', sitePayload(v)),
    onSuccess: (result, submitted) => {
      const revision = result.map_revision ?? submitted.map_revision
      // A slow save must not replace edits made after the submitted snapshot.
      // Keep those values dirty so the follow-up GET cannot hydrate over them.
      form.setValues({ ...form.getValues(), map_revision: revision })
      form.resetDirty({ ...submitted, map_revision: revision })
      setMapConflict(false); toast.ok(t('common.saved'))
      qc.invalidateQueries({ queryKey: ['site-settings'] }); qc.invalidateQueries({ queryKey: publicBrandQueryKey })
    }, onError: error => { if (error instanceof ApiError && error.status === 409) setMapConflict(true); else toast.err(error) },
  })
  const reload = () => modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('mapLocation.siteConflictHint')}</Text>, labels: { confirm: t('serviceStatus.discardReload'), cancel: t('workspace.stay') }, onConfirm: async () => { const result = await q.refetch(); if (!result.isError && result.data) { const fresh = siteValues(result.data); form.setValues(fresh); form.resetDirty(fresh); setMapConflict(false); entries.refetch() } } })
  if (!q.data) return <SettingsLoadState query={q} />
  const missingIDs = missingMapEntryIDs(form.values.map_entry_ids, entries.data)
  const blocked = q.isError || q.isFetching || entries.data === undefined || entries.isError || missingIDs.length > 0 || mapConflict
  const entryChoices = [...(entries.data ?? []).map(entry => ({ value: String(entry.ID), label: entry.Name })), ...missingIDs.map(id => ({ value: String(id), label: t('serviceStatus.deletedEntry', { id }) }))]
  return (
    <>
      {!embedded && <PageHeader title={t('site.title')} subtitle={t('site.subtitle')} actions={<Button component="a" href="/" target="_blank" variant="default">{t('site.open')}</Button>} />}
      <form onSubmit={form.onSubmit((v) => { if (!blocked && !save.isPending) save.mutate(v) }, errors => {
        if (['hub', 'locations'].some(field => errors[field])) setAdvancedOpen(true)
        if (['features', 'faq'].some(field => errors[field])) { setHomepageOpen(true); setHomepageAdvancedOpen(true) }
      })}>
        <Stack>
          {(q.isError || entries.isError) && <Alert color="red"><Text size="sm">{t('workspace.loadError')}</Text><Button variant="light" size="xs" onClick={() => { q.refetch(); entries.refetch() }}>{t('workspace.retry')}</Button></Alert>}
          {mapConflict && <Alert color="yellow"><Text size="sm">{t('mapLocation.siteConflict')}</Text><Button variant="light" size="xs" onClick={reload}>{t('serviceStatus.reload')}</Button></Alert>}
          <Card component="section" id="site-brand" aria-labelledby="site-brand-title" style={{ scrollMarginTop: 88 }}>
            <Title id="site-brand-title" order={5} mb={4}>{t('branding.title')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('branding.hint')}</Text>
            <Stack gap="sm">
              <SettingsFields>
                <TextInput label={t('branding.name')} {...form.getInputProps('name')} />
                <TextInput label={t('branding.browserTitle')} placeholder={form.values.name || 'Captain'} {...form.getInputProps('brand_title')} />
              </SettingsFields>
              <SettingsFields>
                <BrandAssetInput label={t('branding.logoLight')} value={form.values.logo_light} onChange={v => form.setFieldValue('logo_light', v)} />
                <BrandAssetInput label={t('branding.logoDark')} value={form.values.logo_dark} onChange={v => form.setFieldValue('logo_dark', v)} dark />
                <BrandAssetInput label={t('branding.favicon')} value={form.values.favicon} onChange={v => form.setFieldValue('favicon', v)} />
              </SettingsFields>
              <Text size="xs" c="dimmed">{t('branding.uploadHint')}</Text>
              <Text size="xs" c="dimmed">{t('branding.fallbackHint')}</Text>
            </Stack>
          </Card>
          <Card component="section" id="site-content" aria-labelledby="site-content-title" style={{ scrollMarginTop: 88 }}>
            <Title id="site-content-title" order={5} mb="sm">{t('site.homepageTitle')}</Title>
            <Switch label={t('site.homepageEnabled')} description={t('site.homepageHint')} checked={form.values.homepage_enabled} onChange={event => {
              const enabled = event.currentTarget.checked
              form.setFieldValue('homepage_enabled', enabled)
              setHomepageOpen(enabled)
            }} />
            {!form.values.homepage_enabled && <Text size="xs" c="dimmed" mt="sm">{t('site.homepageRetained')}</Text>}
            {missingIDs.length > 0 && entries.data && <Alert color="yellow" mt="sm"><Text size="sm">{t('serviceStatus.deletedHint')}</Text><Button variant="subtle" size="xs" onClick={() => setHomepageOpen(true)}>{t('site.homepageContent')}</Button></Alert>}
            <details open={homepageOpen ?? form.values.homepage_enabled} style={{ marginTop: 16 }}>
              <summary style={{ cursor: 'pointer', fontWeight: 600 }} onClick={event => {
                event.preventDefault()
                setHomepageOpen(!(homepageOpen ?? form.values.homepage_enabled))
              }}>{t('site.homepageContent')}</summary>
              <Stack gap="sm" mt="md">
                <Title order={6}>{t('site.copy')}</Title>
                <TextInput label={t('site.tagline')} {...form.getInputProps('tagline')} />
                <Textarea label={t('site.description')} autosize minRows={2} {...form.getInputProps('description')} />
                <Switch label={t('site.showPlans')} {...form.getInputProps('show_plans', { type: 'checkbox' })} />
                <Select label={t('mapLocation.homeSource')} allowDeselect={false} data={[{ value: 'manual', label: t('mapLocation.manualMap') }, { value: 'entries', label: t('mapLocation.entryMap') }]} {...form.getInputProps('map_source')} />
                {(form.values.map_source === 'entries' || missingIDs.length > 0) && <><MultiSelect label={t('mapLocation.homeEntries')} description={t('mapLocation.homeEntriesHint')} searchable clearable data={entryChoices} value={form.values.map_entry_ids.map(String)} onChange={ids => form.setFieldValue('map_entry_ids', ids.map(Number))} disabled={!entries.data || entries.isError} /><Text size="xs" c="dimmed">{t('mapLocation.emptyMapHint')}</Text></>}
                <Text size="xs" c="dimmed">{t('site.homepageMapHint')}</Text>
                <details open={homepageAdvancedOpen} onToggle={event => setHomepageAdvancedOpen(event.currentTarget.open)}>
                  <summary style={{ cursor: 'pointer', fontWeight: 600 }}>{t('site.sections')}</summary>
                  <Stack gap="sm" mt="sm">
                    <JsonInput label={t('site.features')} description={t('site.featuresHint')} autosize minRows={4} formatOnBlur {...form.getInputProps('features')} />
                    <JsonInput label={t('site.faq')} autosize minRows={3} formatOnBlur {...form.getInputProps('faq')} />
                  </Stack>
                </details>
                <Text size="xs" c="dimmed">{t('site.overrideHint')} <Anchor href="https://github.com/zeptop-dev/captain" target="_blank" rel="noopener noreferrer" size="xs">README</Anchor></Text>
              </Stack>
            </details>
          </Card>
          <Card component="section" id="site-links" aria-labelledby="site-links-title" style={{ scrollMarginTop: 88 }}>
            <Title id="site-links-title" order={5} mb={4}>{t('site.sharedLinks')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('site.sharedLinksHint')}</Text>
            <Stack gap="sm">
              <SettingsFields>
                <TextInput label="Telegram" placeholder="https://t.me/..." {...form.getInputProps('telegram')} />
                <TextInput label={t('site.tos')} {...form.getInputProps('tos')} />
                <TextInput label={t('site.download')} {...form.getInputProps('download')} />
                <TextInput label="GitHub" {...form.getInputProps('github')} />
              </SettingsFields>
            </Stack>
          </Card>
          <Card component="section" id="site-theme" aria-labelledby="site-theme-title" style={{ scrollMarginTop: 88 }}>
            <Title id="site-theme-title" order={5} mb={4}>{t('site.theme')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('site.themeHint')}</Text>
            <Stack gap="sm">
              <SettingsFields>
                <Select label={t('site.primary')} data={['cyan', 'blue', 'indigo', 'violet', 'grape', 'pink', 'red', 'orange', 'yellow', 'lime', 'green', 'teal', 'gray']} allowDeselect={false} {...form.getInputProps('primary')} />
                <Select label={t('site.radius')} data={['xs', 'sm', 'md', 'lg', 'xl']} allowDeselect={false} {...form.getInputProps('radius')} />
                <Select label={t('site.portalScheme')} data={[{ value: 'light', label: t('site.light') }, { value: 'dark', label: t('site.dark') }, { value: 'auto', label: t('site.auto') }]} allowDeselect={false} {...form.getInputProps('scheme')} />
              </SettingsFields>
              <Text size="sm" c="dimmed">{t('site.publicAppearanceHint')}</Text>
              <SettingsFields>
                <TextInput label={t('site.portalTitle')} placeholder={form.values.name} {...form.getInputProps('portal_title')} />
                <TextInput label={t('site.font')} placeholder='"Noto Sans SC", sans-serif' {...form.getInputProps('font_family')} />
              </SettingsFields>
            </Stack>
          </Card>
          <Card component="section" id="site-map" aria-labelledby="site-map-title" style={{ scrollMarginTop: 88 }}>
            <Title id="site-map-title" order={5} mb={4}>{t('site.globe')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('site.globeHint')}</Text>
            <Stack gap="sm">
              <Select label={t('mapLocation.statusSource')} description={t('mapLocation.statusSourceHint')} allowDeselect={false} data={[{ value: 'manual', label: t('mapLocation.manualMap') }, { value: 'entries', label: t('mapLocation.publicEntryMap') }]} {...form.getInputProps('status_map_source')} />
              <Text size="xs" c="dimmed">{t('site.sharedMapHint')}</Text>
              <Anchor component={Link} to="/settings/category/monitoring#service-status" size="sm">{t('serviceStatus.title')}</Anchor>
            </Stack>
          </Card>
          <Card component="section" id="site-advanced" aria-labelledby="site-advanced-title" style={{ scrollMarginTop: 88 }}>
            <details open={advancedOpen} onToggle={event => setAdvancedOpen(event.currentTarget.open)}>
              <summary style={{ cursor: 'pointer', fontWeight: 600 }}><Text component="span" id="site-advanced-title" fw={600}>{t('settingsLayout.advanced')}</Text></summary>
              <Text size="xs" c="dimmed" mt={4}>{t('settingsLayout.advancedHint')}</Text>
              <Stack mt="md" gap="lg">
                <Stack gap="sm">
                  <Title order={6}>{t('mapLocation.manualMap')}</Title>
                  <Text size="xs" c="dimmed">{t('site.sharedMapHint')}</Text>
                  <TextInput label={t('site.hub')} placeholder='{"name":"Example city","lat":35,"lng":135}' {...form.getInputProps('hub')} />
                  <JsonInput label={t('site.locations')} description={t('mapLocation.manualMapHint')} autosize minRows={4} formatOnBlur {...form.getInputProps('locations')} />
                </Stack>
                <Stack gap="sm">
                  <Title order={6}>{t('site.inject')}</Title>
                  <Text size="xs" c="dimmed">{t('site.injectHint')}</Text>
                  <Textarea label={t('site.injectHead')} placeholder="<script ...></script>" autosize minRows={2} ff="monospace" {...form.getInputProps('inject_head')} />
                  <Textarea label={t('site.injectBody')} autosize minRows={2} ff="monospace" {...form.getInputProps('inject_body')} />
                </Stack>
              </Stack>
            </details>
          </Card>
          <Group justify="flex-end"><Button type="submit" loading={save.isPending} disabled={blocked}>{t('settingsLayout.saveSection', { section: t('settingsLayout.categories.site') })}</Button></Group>
        </Stack>
      </form>
    </>
  )
}

function json(v: string) { try { JSON.parse(v || '[]'); return null } catch { return 'invalid JSON' } }
