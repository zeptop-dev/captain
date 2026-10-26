import { Alert, Anchor, Button, Card, Group, Select, Skeleton, Stack, Tabs, Text, Title, TypographyStylesProvider } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { Link, useSearchParams } from 'react-router-dom'
import { useState } from 'react'
import { ClientCatalog, LegacyDownloads } from '../components/ClientCatalog'
import { clientPlatforms, detectClientPlatform, type ClientPlatform, type PublishedClient } from '../../../shared/client-downloads'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import classes from './SecondaryPages.module.css'

interface ArticleHead { id: number; title: string; category: string; lang: string; updated_at: string }
interface Article extends ArticleHead { body: string }
interface Client { name: string; platform: string; url: string; note: string }

// Knowledge base and client downloads. Articles are Markdown from the admin;
// the server already substituted per-user placeholders (subscription URL).
export default function HelpPage() {
  const { t, i18n } = useTranslation()
  const [params, setParams] = useSearchParams()
  const [platform, setPlatform] = useState<ClientPlatform>(() => detectClientPlatform(navigator.userAgent, navigator.maxTouchPoints))
  const list = useQuery({ queryKey: ['articles'], queryFn: () => api.get<ArticleHead[]>('/api/portal/articles') })
  const clients = useQuery({ queryKey: ['clients'], queryFn: () => api.get<Client[]>('/api/portal/clients') })
  const catalog = useQuery({ queryKey: ['client-catalog', 'portal'], queryFn: () => api.get<PublishedClient[]>('/api/portal/client-catalog') })
  const platformClients = (catalog.data ?? []).filter(client => client.platform === platform)
  const platformLegacy = (clients.data ?? []).filter(client => client.platform === platform)
  const articleId = Number(params.get('article'))
  const openId = Number.isSafeInteger(articleId) && articleId > 0 ? articleId : null
  const setOpenId = (id: number | null) => setParams((previous) => { const next = new URLSearchParams(previous); if (id === null) next.delete('article'); else next.set('article', String(id)); return next })
  const article = useQuery({ queryKey: ['article', openId], queryFn: () => api.get<Article>(`/api/portal/articles/${openId}`), enabled: openId !== null })
  const visible = (list.data ?? []).filter((a) => !a.lang || a.lang === i18n.language)
  const cats = Array.from(new Set(visible.map((a) => a.category)))
  const html = article.data ? DOMPurify.sanitize(marked.parse(article.data.body, { async: false }) as string) : ''
  return (
    <Stack className={classes.page}>
      <header className={classes.header}><div><div className={classes.eyebrow}>04 / {t('portalNav.help')}</div><Title order={1} className={classes.title}>{t('help.title')}</Title></div><Button component={Link} to="/tickets" variant="default" rightSection={<span aria-hidden>↗</span>}>{t('tickets.title')}</Button></header>
      <Tabs value={params.get('tab') === 'downloads' ? 'downloads' : 'articles'} onChange={(tab) => setParams((previous) => { const next = new URLSearchParams(previous); next.set('tab', tab || 'articles'); return next })}>
        <Tabs.List><Tabs.Tab value="articles">{t('help.articles')}</Tabs.Tab><Tabs.Tab value="downloads">{t('help.downloads')}</Tabs.Tab></Tabs.List>
        <Tabs.Panel value="articles" pt="md">
          {openId === null ? (
            <Stack gap="md">
              {list.isPending && <Skeleton height={100} radius="lg" />}
              {list.isError && <Alert color="red">{t('accountUI.loadError')} <Button variant="subtle" size="compact-xs" onClick={() => list.refetch()}>{t('auth.retry')}</Button></Alert>}
              {list.isSuccess && visible.length === 0 && <Text c="dimmed">{t('help.empty')}</Text>}
              {cats.map((c) => (
                <div key={c}>
                  {c && <Text size="xs" tt="uppercase" c="dimmed" fw={700} mb={6}>{c}</Text>}
                  <Stack gap={0} className={classes.list}>
                    {visible.filter((a) => a.category === c).map((a) => (
                      <Card key={a.id} component="button" type="button" className={classes.listRow} onClick={() => setOpenId(a.id)}>
                        <Group justify="space-between"><Text className={classes.listTitle}>{a.title}</Text><Text className={classes.listMeta}>{when(a.updated_at).split(',')[0]}</Text></Group>
                      </Card>
                    ))}
                  </Stack>
                </div>
              ))}
            </Stack>
          ) : (
            <Card className={classes.article}>
              <Anchor size="sm" onClick={() => setOpenId(null)}>← {t('help.back')}</Anchor>
              {article.isPending && <Skeleton height={120} mt="md" />}
              {article.isError && <Alert color="red" mt="md">{t('accountUI.loadError')} <Button variant="subtle" size="compact-xs" onClick={() => article.refetch()}>{t('auth.retry')}</Button></Alert>}
              {article.data && <>
                <Title order={3} mt="sm">{article.data.title}</Title>
                <TypographyStylesProvider mt="sm"><div dangerouslySetInnerHTML={{ __html: html }} /></TypographyStylesProvider>
              </>}
            </Card>
          )}
        </Tabs.Panel>
        <Tabs.Panel value="downloads" pt="md">
          <Stack gap="md">
            <Select label={t('clientCatalog.platform')} value={platform} data={clientPlatforms.map(value => ({ value, label: t(`help.platform.${value}`) }))} allowDeselect={false} onChange={value => value && setPlatform(value as ClientPlatform)} maw={320} />
            <Text size="xs" c="dimmed">{t('clientCatalog.architectureHint')}</Text>
            {(clients.isPending || catalog.isPending) && <Skeleton height={140} radius="lg" />}
            {(clients.isError || catalog.isError) && <Alert color="red">{t('accountUI.loadError')} <Button variant="subtle" size="compact-xs" onClick={() => { void clients.refetch(); void catalog.refetch() }}>{t('auth.retry')}</Button></Alert>}
            {clients.isSuccess && catalog.isSuccess && !platformClients.length && !platformLegacy.length && <Text c="dimmed">{t('help.noClients')}</Text>}
            <ClientCatalog clients={platformClients} platform={platform} />
            <LegacyDownloads items={platformLegacy} />
          </Stack>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}
