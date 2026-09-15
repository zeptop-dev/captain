import { Badge, Button, Group, Loader, Stack, Table, Text, Tooltip } from '@mantine/core'
import { IconCheck, IconX } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '../lib/notify'

// One probed REALITY target as the node reports it.
export interface RealityResult {
  host: string; port: number; ip?: string; feasible: boolean; reason?: string
  tls13: boolean; h2: boolean; x25519: boolean; cert_valid: boolean; cdn?: string
  cert_subject?: string; cert_issuer?: string; server_names?: string[]; latency_ms: number
}

// RealityScan runs the node-side probe over the built-in pool ("auto":
// picks the fastest feasible target) or over the host currently typed in,
// and lets the operator pick a row. `scan` is the transport: a direct call
// on the standalone panel, a node job on Captain.
export function RealityScan({ current, scan, onPick }: { current: string; scan: (hosts: string[]) => Promise<RealityResult[]>; onPick: (host: string) => void }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState<'auto' | 'one' | null>(null)
  const [rows, setRows] = useState<RealityResult[] | null>(null)
  const run = async (mode: 'auto' | 'one') => {
    setBusy(mode)
    try {
      const out = await scan(mode === 'one' ? [current.trim()] : [])
      setRows(out)
      if (mode === 'auto') {
        const best = out.find((r) => r.feasible)
        if (best) { onPick(best.host); toast.ok(t('inbounds.realityPicked', { host: best.host, ms: best.latency_ms })) } else toast.err(new Error(t('inbounds.realityNone')))
      }
    } catch (e) { toast.err(e) } finally { setBusy(null) }
  }
  const mark = (v: boolean) => (v ? <IconCheck size={14} color="var(--mantine-color-teal-6)" /> : <IconX size={14} color="var(--mantine-color-red-6)" />)
  return (
    <Stack gap="xs">
      <Group gap="xs">
        <Button size="xs" variant="light" loading={busy === 'auto'} disabled={busy !== null} onClick={() => run('auto')}>{t('inbounds.realityAuto')}</Button>
        <Button size="xs" variant="default" loading={busy === 'one'} disabled={busy !== null || !current.trim()} onClick={() => run('one')}>{t('inbounds.realityProbe')}</Button>
        {busy && <Text size="xs" c="dimmed">{t('inbounds.realityScanning')}</Text>}
        {!busy && !rows && <Text size="xs" c="dimmed">{t('inbounds.realityScanHint')}</Text>}
      </Group>
      {rows && (
        <Table.ScrollContainer minWidth={640}>
          <Table verticalSpacing={4} fz="xs">
            <Table.Thead><Table.Tr>
              <Table.Th>{t('inbounds.handshakeServer')}</Table.Th><Table.Th>{t('inbounds.realityLatency')}</Table.Th>
              <Table.Th>TLS 1.3</Table.Th><Table.Th>h2</Table.Th><Table.Th>X25519</Table.Th><Table.Th>{t('inbounds.realityCert')}</Table.Th>
              <Table.Th>CDN</Table.Th><Table.Th /></Table.Tr></Table.Thead>
            <Table.Tbody>
              {rows.map((r) => (
                <Table.Tr key={r.host + r.port}>
                  <Table.Td>
                    <Text size="xs" fw={600}>{r.host}{r.port !== 443 ? `:${r.port}` : ''}</Text>
                    {r.reason && <Text size="xs" c={r.feasible ? 'dimmed' : 'red'}>{r.reason}</Text>}
                    {r.feasible && r.cert_issuer && <Text size="xs" c="dimmed">{r.cert_issuer}</Text>}
                  </Table.Td>
                  <Table.Td>{r.latency_ms >= 0 ? `${r.latency_ms} ms` : '—'}</Table.Td>
                  <Table.Td>{mark(r.tls13)}</Table.Td><Table.Td>{mark(r.h2)}</Table.Td><Table.Td>{mark(r.x25519)}</Table.Td><Table.Td>{mark(r.cert_valid)}</Table.Td>
                  <Table.Td>{r.cdn ? <Tooltip label={t('inbounds.realityCdnHint')}><Badge color="red" size="xs">{r.cdn}</Badge></Tooltip> : <Text size="xs" c="dimmed">{t('inbounds.realityNoCdn')}</Text>}</Table.Td>
                  <Table.Td>{r.feasible ? <Button size="compact-xs" variant="light" onClick={() => onPick(r.host)}>{t('inbounds.realityUse')}</Button> : <Text size="xs" c="dimmed">{t('inbounds.realityBlocked')}</Text>}</Table.Td>
                </Table.Tr>
              ))}
              {rows.length === 0 && <Table.Tr><Table.Td colSpan={8}><Group gap="xs"><Loader size="xs" /><Text size="xs" c="dimmed">{t('inbounds.realityNone')}</Text></Group></Table.Td></Table.Tr>}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
    </Stack>
  )
}
