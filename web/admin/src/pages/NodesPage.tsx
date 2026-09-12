import { Badge, Button, Card, Code, Group, Modal, Stack, Table, Text, TextInput, Anchor } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useDisclosure } from '@mantine/hooks'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconArrowUp } from '@tabler/icons-react'
import { modals } from '@mantine/modals'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Node, type SystemUpdate } from '../lib/api'
import { ago, bytes } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'

export function PairCodeBox({ code }: { code: string }) {
  const { t } = useTranslation()
  const snippet = `panel:\n  driver: captain\n  captain:\n    url: ${window.location.origin}\n    pair_code: ${code}`
  return (
    <Stack gap="xs">
      <Text size="sm" c="dimmed">{t('nodes.pairHint')}</Text>
      <Group gap="xs"><Code fz="xl" px="md" py={6}>{code}</Code><Copy value={code} /></Group>
      <Text size="xs" c="dimmed" mt="xs">{t('nodes.configSnippet')}</Text>
      <Group align="flex-start" gap="xs"><Code block style={{ flex: 1 }}>{snippet}</Code><Copy value={snippet} /></Group>
    </Stack>
  )
}

export function NodeStatus({ n }: { n: Node }) {
  const { t } = useTranslation()
  if (!n.paired) return <Badge color="gray">{t('nodes.unpaired')}</Badge>
  return n.online ? <Badge color="teal">{t('nodes.online')}</Badge> : <Badge color="red">{t('nodes.offline')}</Badge>
}

export default function NodesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['nodes'], queryFn: () => api.get<Node[]>('/api/admin/nodes'), refetchInterval: 15_000 })
  const [opened, { open, close }] = useDisclosure()
  const [created, setCreated] = useState<Node | null>(null)
  const form = useForm({ initialValues: { Name: '', PublicAddr: '', InternalAddr: '', V6Addr: '', MonitorURL: '' } })
  const sys = useQuery({ queryKey: ['update'], queryFn: () => api.get<SystemUpdate>('/api/admin/system/update'), staleTime: 10 * 60_000, retry: false })
  const upgrade = useMutation({ mutationFn: (id: number) => api.post(`/api/admin/nodes/${id}/upgrade`, {}), onSuccess: () => { toast.ok(t('nodes.upgradeQueued')); qc.invalidateQueries({ queryKey: ['nodes'] }) }, onError: toast.err })
  const upgradeAll = useMutation({ mutationFn: () => api.post<{ nodes: number; upgrade_to: string }>('/api/admin/nodes/upgrade-all', {}), onSuccess: (r) => { toast.ok(t('nodes.upgradeAllQueued', { count: r.nodes, version: r.upgrade_to })); qc.invalidateQueries({ queryKey: ['nodes'] }) }, onError: toast.err })
  const outdated = (q.data ?? []).filter((n) => n.outdated && n.paired).length
  const create = useMutation({
    mutationFn: (v: typeof form.values) => api.post<Node>('/api/admin/nodes', v),
    onSuccess: (n) => { setCreated(n); form.reset(); qc.invalidateQueries({ queryKey: ['nodes'] }) },
    onError: toast.err,
  })
  return (
    <>
      <PageHeader title={t('nodes.title')} subtitle={t('nodes.subtitle')} actions={<>
        {outdated > 0 && <Button variant="light" color="orange" leftSection={<IconArrowUp size={16} />} loading={upgradeAll.isPending} onClick={() => modals.openConfirmModal({ title: t('nodes.upgradeAll'), children: <Text size="sm">{t('nodes.upgradeAllConfirm', { count: outdated, version: sys.data?.bosun_latest ?? '' })}</Text>, labels: { confirm: t('nodes.upgradeAll'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => upgradeAll.mutate() })}>{t('nodes.upgradeAll', { count: outdated })}</Button>}
        <Button leftSection={<IconPlus size={16} />} onClick={open}>{t('nodes.create')}</Button>
      </>} />
      <Card p={0}>
        <Table.ScrollContainer minWidth={720}>
          <Table>
            <Table.Thead><Table.Tr>
              <Table.Th>{t('nodes.name')}</Table.Th><Table.Th>{t('nodes.status')}</Table.Th><Table.Th>{t('nodes.publicAddr')}</Table.Th>
              <Table.Th>{t('nodes.inbounds')}</Table.Th><Table.Th>{t('nodes.trafficToday')}</Table.Th><Table.Th>{t('nodes.version')}</Table.Th><Table.Th>{t('nodes.lastSeen')}</Table.Th>
            </Table.Tr></Table.Thead>
            <Table.Tbody>
              {(q.data ?? []).map((n) => (
                <Table.Tr key={n.id}>
                  <Table.Td><Anchor component={Link} to={`/nodes/${n.id}`} fw={600}>{n.name}</Anchor><Text size="xs" c="dimmed">{n.hostname}</Text></Table.Td>
                  <Table.Td><Group gap={4}><NodeStatus n={n} />{n.cert_problem && <Badge color="red" size="xs" title={t('nodes.certProblem')}>TLS</Badge>}</Group></Table.Td>
                  <Table.Td><Code>{n.public_addr || '—'}</Code></Table.Td>
                  <Table.Td>{n.inbounds}</Table.Td>
                  <Table.Td>{bytes(n.traffic_today_bytes)}</Table.Td>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Text size="sm">{n.version || '—'}</Text>
                      {n.upgrade_to ? <Badge size="xs" color="blue">{t('nodes.upgrading', { version: n.upgrade_to })}</Badge> : n.outdated && n.paired && <Badge size="xs" color="orange" style={{ cursor: 'pointer' }} onClick={() => modals.openConfirmModal({ title: t('nodes.upgrade'), children: <Text size="sm">{t('nodes.upgradeConfirm', { name: n.name, version: sys.data?.bosun_latest ?? '' })}</Text>, labels: { confirm: t('nodes.upgrade'), cancel: t('common.cancel') }, confirmProps: { color: 'orange' }, onConfirm: () => upgrade.mutate(n.id) })}>{t('nodes.outdated', { version: sys.data?.bosun_latest ?? '' })}</Badge>}
                    </Group>
                    <Text size="xs" c="dimmed">{n.platform}</Text>
                  </Table.Td>
                  <Table.Td>{ago(n.last_seen_at)}</Table.Td>
                </Table.Tr>
              ))}
              {q.data?.length === 0 && <Table.Tr><Table.Td colSpan={7}><Text c="dimmed" ta="center" py="lg">{t('common.empty')}</Text></Table.Td></Table.Tr>}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Card>
      <Modal opened={opened} onClose={() => { close(); setCreated(null) }} title={created ? t('nodes.pairTitle') : t('nodes.create')} size="lg">
        {created ? <PairCodeBox code={created.pair_code ?? ''} /> : (
          <form onSubmit={form.onSubmit((v) => create.mutate(v))}>
            <Stack>
              <TextInput label={t('nodes.name')} placeholder="jp-1" required {...form.getInputProps('Name')} />
              <TextInput label={t('nodes.publicAddr')} placeholder="203.0.113.5" {...form.getInputProps('PublicAddr')} />
              <Group grow>
                <TextInput label={t('nodes.internalAddr')} {...form.getInputProps('InternalAddr')} />
                <TextInput label={t('nodes.v6Addr')} {...form.getInputProps('V6Addr')} />
              </Group>
              <TextInput label={t('nodes.monitorUrl')} placeholder="https://komari.example.com/..." {...form.getInputProps('MonitorURL')} />
              <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={create.isPending}>{t('common.create')}</Button></Group>
            </Stack>
          </form>
        )}
      </Modal>
    </>
  )
}
