import { Badge, Card, Group, Stack, Text, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { api, type Order, type Plan } from '../lib/api'
import { useAuth } from '../lib/auth'
import { money, when } from '../lib/format'
import { toast } from '../lib/notify'

const colors: Record<string, string> = { pending: 'orange', paid: 'teal', cancelled: 'gray' }

export default function OrdersPage() {
  const { t } = useTranslation()
  const { refresh } = useAuth()
  const [params] = useSearchParams()
  const q = useQuery({ queryKey: ['orders'], queryFn: () => api.get<Order[]>('/api/portal/orders'), refetchInterval: params.get('paid') ? 3000 : false })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/portal/plans') })
  // Landing here after a gateway redirect: refresh the account.
  useEffect(() => { if (params.get('paid')) { toast.ok(t('plans.paid')); refresh() } }, [params, refresh, t])
  return (
    <Stack gap="lg">
      <Title order={2}>{t('orders.title')}</Title>
      {q.data?.length === 0 && <Text c="dimmed">{t('orders.empty')}</Text>}
      {(q.data ?? []).map((o) => (
        <Card key={o.ID} padding="md">
          <Group justify="space-between">
            <div>
              <Text fw={700}>{plans.data?.find((p) => p.ID === o.PlanID)?.Name ?? `#${o.PlanID}`}</Text>
              <Text size="xs" c="dimmed" ff="monospace">{o.No} · {when(o.CreatedAt)}</Text>
            </div>
            <Group gap="sm"><Text fw={700}>{money(o.AmountCents)}</Text><Badge color={colors[o.Status] ?? 'gray'}>{t(`orders.${o.Status}`)}</Badge></Group>
          </Group>
        </Card>
      ))}
    </Stack>
  )
}
