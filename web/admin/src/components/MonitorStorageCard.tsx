import { Alert, Button, Card, Group, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { bytes } from '../lib/format'
import { toast } from '../lib/notify'
interface Storage { database_bytes: number; wal_bytes: number; shm_bytes: number; free_bytes: number; monitoring_bytes: number | null; size_known: boolean; retention: { last_success?: string; rows: number; duration_ms: number; backlog: string; budget_exhausted: boolean; errors: number } }
export function MonitorStorageCard() {
 const { t } = useTranslation(), qc = useQueryClient()
 const q = useQuery({ queryKey: ['monitor-storage'], queryFn: () => api.get<Storage>('/api/admin/settings/monitor-storage'), refetchInterval: 60_000 })
 const run = useMutation({ mutationFn: () => api.post<{ checkpoint_busy: number }>('/api/admin/settings/monitor-storage/maintain'), onSuccess: () => { qc.invalidateQueries({ queryKey: ['monitor-storage'] }); toast.ok(t('common.saved')) }, onError: toast.err })
 const s = q.data
 return <Card><Stack gap="sm"><Group justify="space-between"><Title order={4}>{t('insights.storage')}</Title><Group><Button variant="subtle" onClick={() => q.refetch()} loading={q.isFetching}>{t('common.refresh')}</Button><Button variant="light" onClick={() => run.mutate()} loading={run.isPending} disabled={!s || q.isError}>{t('insights.maintain')}</Button></Group></Group>
 <Text size="sm" c="dimmed">{t('insights.storageHint')}</Text>{q.isError && <Alert color="red">{q.error.message}</Alert>}
 {s && <><SimpleGrid cols={{ base: 1, sm: 3 }}>{[['database', s.database_bytes], ['wal', s.wal_bytes], ['monitoring', s.monitoring_bytes], ['free', s.free_bytes], ['shm', s.shm_bytes]].map(([k, v]) => <div key={k as string}><Text size="xs" c="dimmed">{t(`insights.${k}`)}</Text><Text fw={600}>{v == null ? '—' : bytes(v as number)}</Text></div>)}</SimpleGrid><Text size="sm">{t('insights.cleanup', { rows: s.retention.rows, ms: s.retention.duration_ms })} · {t(`insights.backlog.${s.retention.backlog}`)}</Text><Text size="xs" c="dimmed">{t('insights.lastSuccess')}: {s.retention.last_success ? new Date(s.retention.last_success).toLocaleString() : '—'}</Text>{s.retention.errors > 0 && <Alert color="orange">{t('insights.cleanupError')}</Alert>}</>}
 {run.isSuccess && run.data.checkpoint_busy > 0 && <Alert color="yellow">{t('insights.checkpointBusy')}</Alert>}
 </Stack></Card>
}
