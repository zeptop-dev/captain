import { ActionIcon, Badge, Button, Code, Group, NumberInput, Stack, Table, Text, Title, Tooltip } from '@mantine/core'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type HwidDevice, type SubRequest } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'

// Devices identified by the x-hwid header (Happ-class clients) and the
// user's own HWID limit override; plus the recent subscription fetches so
// support can see which client, IP and device pulled the link.
export function HwidDevices({ userID, devices, limit, requests }: { userID: number; devices: HwidDevice[]; limit: number | null; requests: SubRequest[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const inval = () => { qc.invalidateQueries({ queryKey: ['user', userID] }); qc.invalidateQueries({ queryKey: ['users'] }) }
  const [lim, setLim] = useState<number | string>(limit ?? '')
  const save = useMutation({ mutationFn: () => api.put(`/api/admin/users/${userID}/hwid-limit`, { Limit: lim === '' ? null : Number(lim) }), onSuccess: () => { toast.ok(t('common.saved')); inval() }, onError: toast.err })
  const kick = useMutation({ mutationFn: (hwid: string) => api.del(`/api/admin/users/${userID}/hwid-devices/${encodeURIComponent(hwid)}`), onSuccess: () => { toast.ok(t('common.deleted')); inval() }, onError: toast.err })
  const [showReqs, setShowReqs] = useState(false)
  return (
    <Stack gap="sm">
      <Title order={6}>{t('hwid.title')}</Title>
      <Text size="xs" c="dimmed">{t('hwid.hint')}</Text>
      <Group align="flex-end" gap="xs">
        <NumberInput label={t('hwid.limit')} description={t('hwid.limitHint')} w={220} min={0} placeholder={t('hwid.followPlan')} value={lim} onChange={setLim} />
        <Button size="xs" loading={save.isPending} onClick={() => save.mutate()}>{t('common.save')}</Button>
      </Group>
      {devices.length ? (
        <Table fz="xs"><Table.Tbody>
          {devices.map((d) => (
            <Table.Tr key={d.hwid}>
              <Table.Td><Tooltip label={d.hwid}><Code>{d.hwid.slice(0, 12)}…</Code></Tooltip></Table.Td>
              <Table.Td>{[d.platform, d.os_version].filter(Boolean).join(' ')}{d.device_model ? ' · ' + d.device_model : ''}</Table.Td>
              <Table.Td><Text size="xs" c="dimmed" title={d.user_agent}>{(d.user_agent ?? '').split(' ')[0]} · {d.request_ip}</Text></Table.Td>
              <Table.Td><Text size="xs" c="dimmed">{when(d.last_seen_at)}</Text></Table.Td>
              <Table.Td w={36}><ActionIcon size="sm" variant="subtle" color="red" title={t('hwid.kick')} onClick={() => kick.mutate(d.hwid)}><IconTrash size={14} /></ActionIcon></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      ) : <Text size="sm" c="dimmed">{t('hwid.none')}</Text>}
      <Group gap="xs">
        <Button size="compact-xs" variant="subtle" onClick={() => setShowReqs(!showReqs)}>{showReqs ? t('hwid.hideRequests') : t('hwid.showRequests', { n: requests.length })}</Button>
      </Group>
      {showReqs && (
        <Table fz="xs"><Table.Tbody>
          {requests.length === 0 && <Table.Tr><Table.Td><Text size="xs" c="dimmed">{t('hwid.noRequests')}</Text></Table.Td></Table.Tr>}
          {requests.map((r) => (
            <Table.Tr key={r.id}>
              <Table.Td><Text size="xs" c="dimmed">{when(r.at)}</Text></Table.Td>
              <Table.Td><Badge size="xs" variant="light" color={r.response.startsWith('hwid-') || r.response === 'blocked' ? 'red' : 'teal'}>{r.response}</Badge>{r.rule ? <Text span size="xs" c="dimmed"> · {r.rule}</Text> : null}</Table.Td>
              <Table.Td>{r.request_ip}</Table.Td>
              <Table.Td><Text size="xs" c="dimmed" title={r.user_agent} style={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.user_agent}</Text></Table.Td>
              <Table.Td>{r.hwid ? <Tooltip label={r.hwid}><Code>{r.hwid.slice(0, 8)}…</Code></Tooltip> : null}</Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Stack>
  )
}
