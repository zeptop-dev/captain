import { Alert, Badge, Card, Group, Table, Text, Title } from '@mantine/core'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { DoctorReport } from '../lib/api'
import { when } from '../lib/format'
// The node's last self-check (bosun >= 0.18 sends one on change or every 30 min).
export function DoctorCard({ report }: { report: DoctorReport }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(report.summary.fail > 0 || report.summary.warn > 0)
  const color = (s: string) => (s === 'ok' ? 'teal' : s === 'warn' ? 'orange' : s === 'fail' ? 'red' : 'gray')
  // Inbounds the agent left out (no core, missing key, waiting for users):
  // the one thing an operator must act on, so it gets its own bar.
  const notApplied = report.checks.filter((c) => (c.detail ?? '').startsWith('not applied'))
  return (
    <Card mb="lg">
      {notApplied.length > 0 && (
        <Alert color="yellow" variant="light" mb="sm" title={t('nodes.doctorNotApplied')}>
          {notApplied.map((c) => <Text size="sm" key={c.id}><b>{c.id.replace(/^inbound:/, '')}</b>: {(c.detail ?? '').replace(/^not applied: /, '')}</Text>)}
        </Alert>
      )}
      <Group justify="space-between" mb={open ? 'xs' : 0} style={{ cursor: 'pointer' }} onClick={() => setOpen((o) => !o)}>
        <Group gap="sm"><Title order={5}>{t('nodes.doctor')}</Title>
          <Badge color="teal" variant="light" size="xs">{t('nodes.doctorOk')} {report.summary.ok}</Badge>
          {report.summary.warn > 0 && <Badge color="orange" variant="light" size="xs">{t('nodes.doctorWarn')} {report.summary.warn}</Badge>}
          {report.summary.fail > 0 && <Badge color="red" variant="light" size="xs">{t('nodes.doctorFail')} {report.summary.fail}</Badge>}
        </Group>
        <Text size="xs" c="dimmed">{when(report.at)}</Text>
      </Group>
      {open && (
        <Table fz="sm"><Table.Tbody>
          {report.checks.filter((c) => c.status !== 'skip').map((c) => (
            <Table.Tr key={c.id}><Table.Td w={70}><Badge size="xs" color={color(c.status)} variant="light">{c.status}</Badge></Table.Td><Table.Td><Text size="sm">{c.name}</Text></Table.Td><Table.Td><Text size="xs" c="dimmed">{c.detail}</Text></Table.Td></Table.Tr>
          ))}
        </Table.Tbody></Table>
      )}
    </Card>
  )
}
