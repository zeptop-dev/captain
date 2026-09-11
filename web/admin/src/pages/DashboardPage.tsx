import { Card, Group, SimpleGrid, Skeleton, Text } from '@mantine/core'
import { AreaChart } from '@mantine/charts'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { IconDevices, IconServer, IconArrowsExchange, IconCoin } from '@tabler/icons-react'
import { api, type Dashboard } from '../lib/api'
import { bytes, money } from '../lib/format'
import { PageHeader } from '../components/PageHeader'
import { Stat } from '../components/Stat'

export default function DashboardPage() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['dashboard'], queryFn: () => api.get<Dashboard>('/api/admin/dashboard'), refetchInterval: 10_000 })
  const s = q.data?.stats
  const down = s ? s.nodes - s.nodes_online : 0
  const series = (q.data?.traffic ?? []).map((d) => ({ day: new Date(d.day * 1000).toLocaleDateString(undefined, { month: 'numeric', day: 'numeric' }), GiB: +((d.up + d.down) / 2 ** 30).toFixed(2) }))
  return (
    <>
      <PageHeader title={t('dashboard.title')} subtitle={s ? t('dashboard.subtitle', { online: s.nodes_online, nodes: s.nodes, active: s.active_subs }) : undefined} />
      {!s ? <Skeleton h={120} /> : (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="lg">
          <Stat label={t('dashboard.onlineDevices')} value={s.online_devices} icon={<IconDevices size={18} opacity={0.6} />} />
          <Stat label={t('dashboard.activeSubs')} value={s.active_subs} hint={t('dashboard.ofUsers', { count: s.users })} icon={<IconArrowsExchange size={18} opacity={0.6} />} />
          <Stat label={t('dashboard.trafficToday')} value={bytes(s.traffic_today_bytes)} icon={<IconArrowsExchange size={18} opacity={0.6} />} />
          <Stat label={t('dashboard.revenueToday')} value={money(s.revenue_today_cents)} hint={<>{t('dashboard.revenueMonth', { amount: money(s.revenue_month_cents) })} · {t('dashboard.pendingOrders', { count: s.orders_pending })}</>} icon={<IconCoin size={18} opacity={0.6} />} />
          <Stat label={t('dashboard.nodesOnline')} value={<>{s.nodes_online}<Text span c="dimmed" fz="lg"> / {s.nodes}</Text></>}
            hint={down === 0 ? t('dashboard.allUp') : t('dashboard.someDown', { count: down })} color={down > 0 ? 'orange' : undefined} icon={<IconServer size={18} opacity={0.6} />} />
        </SimpleGrid>
      )}
      <Card>
        <Group justify="space-between" mb="md">
          <div>
            <Text fw={600}>{t('dashboard.trafficChart')}</Text>
            <Text size="xs" c="dimmed">{t('dashboard.trafficChartSub')}</Text>
          </div>
        </Group>
        <AreaChart h={240} data={series} dataKey="day" series={[{ name: 'GiB', color: 'cyan.5' }]} curveType="monotone" withDots={false} gridAxis="x" />
      </Card>
    </>
  )
}
