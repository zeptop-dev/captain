import { Badge, Card, Code, Group, Pagination, SegmentedControl, Table, Text } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Order, type Page } from '../lib/api'
import { money, when } from '../lib/format'
import { PageHeader } from '../components/PageHeader'

const colors: Record<string, string> = { pending: 'orange', paid: 'teal', cancelled: 'gray' }

export default function OrdersPage() {
  const { t } = useTranslation()
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const q = useQuery({ queryKey: ['orders', status, page], queryFn: () => api.get<Page<Order>>(`/api/admin/orders?status=${status}&page=${page}`) })
  const pages = q.data ? Math.max(1, Math.ceil(q.data.total / q.data.per_page)) : 1
  return (
    <>
      <PageHeader title={t('orders.title')} subtitle={t('orders.subtitle')} actions={<SegmentedControl size="xs" value={status} onChange={(v) => { setStatus(v); setPage(1) }} data={[{ value: '', label: t('common.all') }, { value: 'pending', label: t('orders.pending') }, { value: 'paid', label: t('orders.paid') }, { value: 'cancelled', label: t('orders.cancelled') }]} />} />
      <Card p={0}>
        <Table.ScrollContainer minWidth={760}><Table>
          <Table.Thead><Table.Tr><Table.Th>{t('orders.no')}</Table.Th><Table.Th>{t('orders.user')}</Table.Th><Table.Th>{t('orders.plan')}</Table.Th><Table.Th>{t('orders.amount')}</Table.Th><Table.Th>{t('orders.gateway')}</Table.Th><Table.Th>{t('orders.status')}</Table.Th><Table.Th>{t('orders.createdAt')}</Table.Th><Table.Th>{t('orders.paidAt')}</Table.Th></Table.Tr></Table.Thead>
          <Table.Tbody>
            {(q.data?.items ?? []).map((o) => (
              <Table.Tr key={o.ID}>
                <Table.Td><Code>{o.No}</Code></Table.Td><Table.Td>{o.email}</Table.Td><Table.Td>{o.plan_name}</Table.Td><Table.Td>{money(o.AmountCents)}</Table.Td>
                <Table.Td>{o.Gateway}{o.GatewayRef && <Text size="xs" c="dimmed">{o.GatewayRef}</Text>}</Table.Td>
                <Table.Td><Badge color={colors[o.Status] ?? 'gray'}>{t(`orders.${o.Status}`)}</Badge></Table.Td>
                <Table.Td>{when(o.CreatedAt)}</Table.Td><Table.Td>{when(o.PaidAt)}</Table.Td>
              </Table.Tr>
            ))}
            {q.data?.items.length === 0 && <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
          </Table.Tbody>
        </Table></Table.ScrollContainer>
        {pages > 1 && <Group justify="center" p="md"><Pagination total={pages} value={page} onChange={setPage} /></Group>}
      </Card>
    </>
  )
}
