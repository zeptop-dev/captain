import { Alert, Anchor, Badge, Button, Card, Group, SimpleGrid, Skeleton, Stack, Tabs, Text, Title, TypographyStylesProvider } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { Link, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import classes from './SecondaryPages.module.css'

interface ArticleHead { id: number; title: string; category: string; lang: string; updated_at: string }
interface Article extends ArticleHead { body: string }
interface Client { name: string; platform: string; url: string; note: string }

const platforms = ['windows', 'macos', 'ios', 'android', 'linux', 'other']

// Knowledge base and client downloads. Articles are Markdown from the admin;
// the server already substituted per-user placeholders (subscription URL).
export default function HelpPage() {
  const { t, i18n } = useTranslation()
  const [params, setParams] = useSearchParams()
  const list = useQuery({ queryKey: ['articles'], queryFn: () => api.get<ArticleHead[]>('/api/portal/articles') })
  const clients = useQuery({ queryKey: ['clients'], queryFn: () => api.get<Client[]>('/api/portal/clients') })
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
          {clients.isPending && <Skeleton height={140} radius="lg" />}
          {clients.isError && <Alert color="red">{t('accountUI.loadError')} <Button variant="subtle" size="compact-xs" onClick={() => clients.refetch()}>{t('auth.retry')}</Button></Alert>}
          {clients.isSuccess && (clients.data ?? []).length === 0 && <Text c="dimmed">{t('help.noClients')}</Text>}
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
            {platforms.flatMap((p) => (clients.data ?? []).filter((c) => c.platform === p)).map((c, i) => (
              <Card key={i} className={classes.download} component="a" href={c.url} target="_blank" rel="noreferrer">
                <Group justify="space-between" wrap="nowrap"><div><Text fw={600}>{c.name}</Text>{c.note && <Text size="xs" c="dimmed">{c.note}</Text>}</div><Badge variant="light">{t(`help.platform.${c.platform}`, { defaultValue: c.platform })}</Badge></Group>
              </Card>
            ))}
          </SimpleGrid>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}
