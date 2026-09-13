import { Accordion, Anchor, Badge, Box, Button, Card, Container, Group, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { IconBolt, IconShieldLock, IconWorld, IconDevices, IconRefresh, IconHeadset, IconBrandTelegram, IconArrowRight } from '@tabler/icons-react'
import { NodeGlobe } from './components/Globe'
import { getJSON, type Plan, type Site } from './lib/site'
import { bytes, money } from './lib/format'

const icons = { bolt: IconBolt, shield: IconShieldLock, world: IconWorld, devices: IconDevices, refresh: IconRefresh, headset: IconHeadset }

export default function App() {
  const site = useQuery({ queryKey: ['site'], queryFn: () => getJSON<Site>('/api/site') })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => getJSON<Plan[]>('/api/portal/plans'), enabled: !!site.data?.show_plans })
  const s = site.data
  if (!s) return null
  return (
    <Box pos="relative" style={{ zIndex: 1 }}>
      <div className="site-bg" />
      <Container size="lg" py="md">
        <Group justify="space-between">
          <Group gap="xs"><Text fw={800} fz="xl" className="gradient-text">{s.name}</Text></Group>
          <Group gap="sm">
            {s.probe_url && <Anchor href={s.probe_url} c="dimmed" size="sm">服务状态</Anchor>}
            {s.links.telegram && <Anchor href={s.links.telegram} c="dimmed" size="sm" target="_blank"><Group gap={4}><IconBrandTelegram size={16} />Telegram</Group></Anchor>}
            <Button component="a" href="/portal/login" variant="default" size="sm">登录</Button>
            {s.registration && <Button component="a" href="/portal/register" size="sm">注册</Button>}
          </Group>
        </Group>
      </Container>

      <Box pos="relative" py={{ base: 40, md: 80 }}>
        <div className="grid-bg" />
        <Container size="lg" pos="relative">
          <SimpleGrid cols={{ base: 1, md: 2 }} spacing={40} style={{ alignItems: 'center' }}>
            <Stack gap="lg">
              <Badge variant="light" color="teal" size="lg" leftSection={<span className="live-dot" />}>{s.stats.online} / {s.stats.nodes} 节点在线 · {s.stats.locations} 个地区</Badge>
              <Title order={1} fz={{ base: 40, md: 56 }} lh={1.1} fw={800}>{s.tagline || s.name}</Title>
              <Text fz="lg" c="dimmed" maw={520}>{s.description}</Text>
              <Group>
                <Button component="a" href={s.registration ? '/portal/register' : '/portal/login'} size="lg" rightSection={<IconArrowRight size={18} />}>{s.registration ? '立即开始' : '登录'}</Button>
                {s.show_plans && <Button component="a" href="#plans" size="lg" variant="default">查看套餐</Button>}
              </Group>
            </Stack>
            <NodeGlobe locations={s.locations} hub={s.hub} />
          </SimpleGrid>
        </Container>
      </Box>

      {s.features.length > 0 && (
        <Container size="lg" py={60}>
          <SimpleGrid cols={{ base: 1, sm: 2, md: 3 }} spacing="lg">
            {s.features.map((f, i) => { const I = icons[(f.icon as keyof typeof icons) ?? 'bolt'] ?? IconBolt; return (
              <Card key={i} className="glass" bg="transparent" withBorder={false}>
                <I size={28} color="#67e8f9" />
                <Text fw={700} mt="sm">{f.title}</Text>
                <Text size="sm" c="dimmed" mt={4}>{f.text}</Text>
              </Card>
            ) })}
          </SimpleGrid>
        </Container>
      )}

      {s.show_plans && (plans.data ?? []).length > 0 && (
        <Container size="lg" py={60} id="plans">
          <Title order={2} ta="center" mb="xl">套餐</Title>
          <SimpleGrid cols={{ base: 1, sm: 2, md: Math.min(4, plans.data!.length) }} spacing="lg">
            {plans.data!.map((p) => (
              <Card key={p.ID} className="glass" bg="transparent" withBorder={false}>
                <Text fw={700} fz="lg">{p.Name}</Text>
                <Text fz={36} fw={800} mt="xs" className="gradient-text">{money(p.PriceCents)}<Text span fz="sm" c="dimmed"> / {p.PeriodDays ? `${p.PeriodDays} 天` : '永久'}</Text></Text>
                <Stack gap={4} mt="md" c="dimmed" fz="sm">
                  <Text>{p.QuotaBytes ? `${bytes(p.QuotaBytes)} 流量` : '不限流量'}</Text>
                  <Text>{p.DeviceLimit ? `${p.DeviceLimit} 台设备` : '设备数不限'}</Text>
                  {p.SpeedLimitMbps > 0 && <Text>{p.SpeedLimitMbps} Mbps</Text>}
                </Stack>
                <Button component="a" href="/portal/plans" mt="lg" fullWidth variant="light">选择</Button>
              </Card>
            ))}
          </SimpleGrid>
        </Container>
      )}

      {s.faq.length > 0 && (
        <Container size="sm" py={60}>
          <Title order={2} ta="center" mb="xl">常见问题</Title>
          <Accordion variant="separated">
            {s.faq.map((f, i) => <Accordion.Item key={i} value={String(i)}><Accordion.Control>{f.q}</Accordion.Control><Accordion.Panel><Text c="dimmed" size="sm">{f.a}</Text></Accordion.Panel></Accordion.Item>)}
          </Accordion>
        </Container>
      )}

      <Container size="lg" py={40}>
        <Group justify="space-between">
          <Text size="sm" c="dimmed">© {new Date().getFullYear()} {s.name}</Text>
          <Group gap="md">
            {s.links.tos && <Anchor href={s.links.tos} size="sm" c="dimmed">服务条款</Anchor>}
            {s.links.download && <Anchor href={s.links.download} size="sm" c="dimmed">客户端下载</Anchor>}
            <Anchor href="/portal/" size="sm" c="dimmed">用户中心</Anchor>
          </Group>
        </Group>
      </Container>
    </Box>
  )
}
