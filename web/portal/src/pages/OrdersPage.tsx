import { Alert, Badge, Button, Card, Group, Skeleton, Stack, Text, Title } from '@mantine/core'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { api, type Order, type Plan } from '../lib/api'
import { money, when } from '../lib/format'

const colors: Record<string, string> = { pending: 'orange', paid: 'teal', cancelled: 'gray' }

export default function OrdersPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [params] = useSearchParams()
  const returned = !!params.get('paid')
  const q = useQuery({ queryKey: ['orders'], queryFn: () => api.get<Order[]>('/api/portal/orders'), refetchInterval: (query) => returned && query.state.data?.some((order) => order.Status === 'pending') ? 3000 : false })
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/portal/plans') })
  // A gateway redirect is not proof of payment. Only the fetched order status is authoritative.
  useEffect(() => { if (returned && q.isSuccess) void qc.invalidateQueries({ queryKey: ['me'] }) }, [returned, q.dataUpdatedAt, q.isSuccess, qc])
  return (
    <Stack gap="lg">
      <Title order={2}>{t('orders.title')}</Title>
      {returned && <Alert color="blue">{t('accountUI.paymentReturn')}</Alert>}
      {q.isPending && <Skeleton height={140} radius="lg" />}
      {q.isError && <Alert color="red">{t('accountUI.loadError')} <Button variant="subtle" size="compact-xs" onClick={() => q.refetch()}>{t('auth.retry')}</Button></Alert>}
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
