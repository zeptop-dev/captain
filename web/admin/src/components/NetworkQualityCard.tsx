import { Accordion, Alert, Card, Button, Group, Text, Loader, Select, Stack, Title } from '@mantine/core'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { ProbePlanSummary } from './NodeProbePlanCard'
import { api } from '../lib/api'
import type { NetworkHistory } from '../lib/network'
import { NetworkQualityTable } from './NetworkQualityTable'
import { NetworkHistoryChart } from './NetworkHistoryChart'
export function NetworkQualityCard({ nodeID, compact = false }: { nodeID: number; compact?: boolean }) {
 const { t } = useTranslation()
 const { me } = useAuth()
 const [range, setRange] = useState('24h')
 const q = useQuery({ queryKey: ['network-quality', nodeID, range], queryFn: () => api.get<NetworkHistory>(`/api/admin/nodes/${nodeID}/network-quality?range=${range}`), refetchInterval: 30_000 })
 const history = <><Select label={t('networkQuality.range')} w={180} value={range} onChange={(v) => v && setRange(v)} allowDeselect={false} data={['1h', '24h', '7d', '14d', '30d', '90d']} /><NetworkHistoryChart history={q.data} /></>
 return <Card mb="lg"><Stack gap="sm"><Title order={5}>{t('networkQuality.title')}</Title>
  {(q.data?.probe_plan || me?.role === 'admin') && <Group justify="space-between" align="flex-start">{q.data?.probe_plan && !q.isError && <ProbePlanSummary plan={{ effective: q.data.probe_plan }} />}{me?.role === 'admin' && <Button component={Link} to={`/nodes/${nodeID}/settings/monitoring`} size="compact-xs" variant="subtle">{t('nodeProbePlan.edit')}</Button>}</Group>}
  {q.data?.probe_plan?.enabled && !q.isLoading && !q.isError && !q.data.current.some(sample => sample.task_id === 0) && <Text size="sm" c="dimmed">{t('nodeProbePlan.waiting')}</Text>}
  {q.isError && <Alert color="red">{t('networkQuality.error')}</Alert>}
  {q.isLoading ? <Loader size="sm" /> : <NetworkQualityTable samples={q.data?.current ?? []} paths={q.data?.task_paths} />}
  {compact ? <Accordion variant="default"><Accordion.Item value="history"><Accordion.Control>{t('serverDetail.networkHistory')}</Accordion.Control><Accordion.Panel>{history}</Accordion.Panel></Accordion.Item></Accordion> : history}
 </Stack></Card>
}
