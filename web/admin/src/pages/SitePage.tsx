import { Button, Card, Group, JsonInput, Select, Stack, Switch, Text, TextInput, Textarea, Title, Anchor } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type SiteSettings } from '../lib/api'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'

type Values = { name: string; tagline: string; description: string; show_plans: boolean; telegram: string; tos: string; download: string; github: string; hub: string; locations: string; features: string; faq: string; primary: string; radius: string; scheme: string; site_scheme: string; font_family: string; portal_title: string; inject_head: string; inject_body: string }

export default function SitePage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['site-settings'], queryFn: () => api.get<SiteSettings>('/api/admin/settings/site') })
  const form = useForm<Values>({
    initialValues: { name: '', tagline: '', description: '', show_plans: true, telegram: '', tos: '', download: '', github: '', hub: '', locations: '[]', features: '[]', faq: '[]', primary: 'cyan', radius: 'lg', scheme: 'light', site_scheme: 'dark', font_family: '', portal_title: '', inject_head: '', inject_body: '' },
    validate: { locations: json, features: json, faq: json, hub: (v) => (v.trim() === '' ? null : json(v)) },
  })
  useEffect(() => {
    const s = q.data
    if (!s) return
    form.setValues({ name: s.name, tagline: s.tagline, description: s.description, show_plans: s.show_plans, telegram: s.links.telegram ?? '', tos: s.links.tos ?? '', download: s.links.download ?? '', github: s.links.github ?? '',
      hub: s.hub ? JSON.stringify(s.hub) : '', locations: JSON.stringify(s.locations, null, 2), features: JSON.stringify(s.features, null, 2), faq: JSON.stringify(s.faq, null, 2),
      primary: s.theme?.primary || 'cyan', radius: s.theme?.radius || 'lg', scheme: s.theme?.scheme || 'light', site_scheme: s.theme?.site_scheme || 'dark', font_family: s.theme?.font_family ?? '', portal_title: s.theme?.portal_title ?? '', inject_head: s.inject_head ?? '', inject_body: s.inject_body ?? '' })
  }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = useMutation({
    mutationFn: (v: Values) => api.put('/api/admin/settings/site', {
      name: v.name, tagline: v.tagline, description: v.description, show_plans: v.show_plans,
      links: { telegram: v.telegram, tos: v.tos, download: v.download, github: v.github },
      hub: v.hub.trim() ? JSON.parse(v.hub) : null, locations: JSON.parse(v.locations), features: JSON.parse(v.features), faq: JSON.parse(v.faq),
      theme: { primary: v.primary, radius: v.radius, scheme: v.scheme, site_scheme: v.site_scheme, font_family: v.font_family, portal_title: v.portal_title }, inject_head: v.inject_head, inject_body: v.inject_body,
    }),
    onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['site-settings'] }) }, onError: toast.err,
  })
  return (
    <>
      <PageHeader title={t('site.title')} subtitle={t('site.subtitle')} actions={<Button component="a" href="/" target="_blank" variant="default">{t('site.open')}</Button>} />
      <form onSubmit={form.onSubmit((v) => save.mutate(v))}>
        <Stack>
          <Card>
            <Title order={5} mb="sm">{t('site.copy')}</Title>
            <Stack gap="sm">
              <Group grow><TextInput label={t('site.name')} {...form.getInputProps('name')} /><TextInput label={t('site.tagline')} {...form.getInputProps('tagline')} /></Group>
              <Textarea label={t('site.description')} autosize minRows={2} {...form.getInputProps('description')} />
              <Group grow>
                <TextInput label="Telegram" placeholder="https://t.me/..." {...form.getInputProps('telegram')} />
                <TextInput label={t('site.tos')} {...form.getInputProps('tos')} />
                <TextInput label={t('site.download')} {...form.getInputProps('download')} />
              </Group>
              <Switch label={t('site.showPlans')} {...form.getInputProps('show_plans', { type: 'checkbox' })} />
            </Stack>
          </Card>
          <Card>
            <Title order={5} mb={4}>{t('site.theme')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('site.themeHint')}</Text>
            <Stack gap="sm">
              <Group grow>
                <Select label={t('site.primary')} data={['cyan', 'blue', 'indigo', 'violet', 'grape', 'pink', 'red', 'orange', 'yellow', 'lime', 'green', 'teal', 'gray']} allowDeselect={false} {...form.getInputProps('primary')} />
                <Select label={t('site.radius')} data={['xs', 'sm', 'md', 'lg', 'xl']} allowDeselect={false} {...form.getInputProps('radius')} />
                <Select label={t('site.portalScheme')} data={[{ value: 'light', label: t('site.light') }, { value: 'dark', label: t('site.dark') }, { value: 'auto', label: t('site.auto') }]} allowDeselect={false} {...form.getInputProps('scheme')} />
                <Select label={t('site.siteScheme')} data={[{ value: 'dark', label: t('site.dark') }, { value: 'light', label: t('site.light') }, { value: 'auto', label: t('site.auto') }]} allowDeselect={false} {...form.getInputProps('site_scheme')} />
              </Group>
              <Group grow>
                <TextInput label={t('site.portalTitle')} placeholder={form.values.name} {...form.getInputProps('portal_title')} />
                <TextInput label={t('site.font')} placeholder='"Noto Sans SC", sans-serif' {...form.getInputProps('font_family')} />
              </Group>
            </Stack>
          </Card>
          <Card>
            <Title order={5} mb={4}>{t('site.inject')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('site.injectHint')}</Text>
            <Stack gap="sm">
              <Textarea label={t('site.injectHead')} placeholder="<script ...></script>" autosize minRows={2} ff="monospace" {...form.getInputProps('inject_head')} />
              <Textarea label={t('site.injectBody')} autosize minRows={2} ff="monospace" {...form.getInputProps('inject_body')} />
            </Stack>
          </Card>
          <Card>
            <Title order={5} mb={4}>{t('site.globe')}</Title>
            <Text size="xs" c="dimmed" mb="sm">{t('site.globeHint')}</Text>
            <Stack gap="sm">
              <TextInput label={t('site.hub')} placeholder='{"name":"Shanghai","lat":31.23,"lng":121.47}' {...form.getInputProps('hub')} />
              <JsonInput label={t('site.locations')} autosize minRows={4} formatOnBlur {...form.getInputProps('locations')} />
            </Stack>
          </Card>
          <Card>
            <Title order={5} mb="sm">{t('site.sections')}</Title>
            <Stack gap="sm">
              <JsonInput label={t('site.features')} description={t('site.featuresHint')} autosize minRows={4} formatOnBlur {...form.getInputProps('features')} />
              <JsonInput label={t('site.faq')} autosize minRows={3} formatOnBlur {...form.getInputProps('faq')} />
            </Stack>
          </Card>
          <Text size="xs" c="dimmed">{t('site.overrideHint')} <Anchor href="https://github.com/zeptop-dev/captain" target="_blank" size="xs">README</Anchor></Text>
          <Group justify="flex-end"><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
        </Stack>
      </form>
    </>
  )
}

function json(v: string) { try { JSON.parse(v || '[]'); return null } catch { return 'invalid JSON' } }
