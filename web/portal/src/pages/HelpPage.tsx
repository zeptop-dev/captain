import { Anchor, Badge, Card, Group, SimpleGrid, Stack, Tabs, Text, Title, TypographyStylesProvider } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'

interface ArticleHead { id: number; title: string; category: string; lang: string; updated_at: string }
interface Article extends ArticleHead { body: string }
interface Client { name: string; platform: string; url: string; note: string }

const platforms = ['windows', 'macos', 'ios', 'android', 'linux', 'other']

// Knowledge base and client downloads. Articles are Markdown from the admin;
// the server already substituted per-user placeholders (subscription URL).
export default function HelpPage() {
  const { t, i18n } = useTranslation()
  const list = useQuery({ queryKey: ['articles'], queryFn: () => api.get<ArticleHead[]>('/api/portal/articles') })
  const clients = useQuery({ queryKey: ['clients'], queryFn: () => api.get<Client[]>('/api/portal/clients') })
  const [openId, setOpenId] = useState<number | null>(null)
  const article = useQuery({ queryKey: ['article', openId], queryFn: () => api.get<Article>(`/api/portal/articles/${openId}`), enabled: openId !== null })
  const visible = (list.data ?? []).filter((a) => !a.lang || a.lang === i18n.language)
  const cats = Array.from(new Set(visible.map((a) => a.category)))
  const html = article.data ? DOMPurify.sanitize(marked.parse(article.data.body, { async: false }) as string) : ''
  return (
    <Stack gap="lg">
      <Title order={2}>{t('help.title')}</Title>
      <Tabs defaultValue="articles">
        <Tabs.List><Tabs.Tab value="articles">{t('help.articles')}</Tabs.Tab><Tabs.Tab value="downloads">{t('help.downloads')}</Tabs.Tab></Tabs.List>
        <Tabs.Panel value="articles" pt="md">
          {openId === null ? (
            <Stack gap="md">
              {visible.length === 0 && <Text c="dimmed">{t('help.empty')}</Text>}
              {cats.map((c) => (
                <div key={c}>
                  {c && <Text size="xs" tt="uppercase" c="dimmed" fw={700} mb={6}>{c}</Text>}
                  <Stack gap="xs">
                    {visible.filter((a) => a.category === c).map((a) => (
                      <Card key={a.id} padding="sm" style={{ cursor: 'pointer' }} onClick={() => setOpenId(a.id)}>
                        <Group justify="space-between"><Text fw={600}>{a.title}</Text><Text size="xs" c="dimmed">{when(a.updated_at).split(',')[0]}</Text></Group>
                      </Card>
                    ))}
                  </Stack>
                </div>
              ))}
            </Stack>
          ) : (
            <Card>
              <Anchor size="sm" onClick={() => setOpenId(null)}>← {t('help.back')}</Anchor>
              {article.data && <>
                <Title order={3} mt="sm">{article.data.title}</Title>
                <TypographyStylesProvider mt="sm"><div dangerouslySetInnerHTML={{ __html: html }} /></TypographyStylesProvider>
              </>}
            </Card>
          )}
        </Tabs.Panel>
        <Tabs.Panel value="downloads" pt="md">
          {(clients.data ?? []).length === 0 && <Text c="dimmed">{t('help.noClients')}</Text>}
          <SimpleGrid cols={{ base: 1, xs: 2 }}>
            {platforms.flatMap((p) => (clients.data ?? []).filter((c) => c.platform === p)).map((c, i) => (
              <Card key={i} padding="sm" component="a" href={c.url} target="_blank" rel="noreferrer">
                <Group justify="space-between" wrap="nowrap"><div><Text fw={600}>{c.name}</Text>{c.note && <Text size="xs" c="dimmed">{c.note}</Text>}</div><Badge variant="light">{t(`help.platform.${c.platform}`, { defaultValue: c.platform })}</Badge></Group>
              </Card>
            ))}
          </SimpleGrid>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}
