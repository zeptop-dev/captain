import { Alert, Badge, Button, Card, Group, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

type Readiness = {
  node_id: number; name: string; ready: boolean; supported: boolean; fresh: boolean; reported_at: number
  checks: { id: string; ok: boolean }[]; installable: boolean; packages: string[]
  job?: { id: string; done_at?: string; error?: string; result?: { error_code?: string }; params: { action: string } }
}
export function useWireGuardReadiness(nodeID: number, transit = false) {
  return useQuery({ queryKey: ['wireguard-readiness', nodeID, transit], enabled: nodeID > 0,
    queryFn: () => api.get<Readiness>(`/api/admin/settings/nodes/${nodeID}/wireguard-readiness?transit=${transit}`),
    refetchInterval: q => q.state.data?.job && !q.state.data.job.done_at ? 2000 : 15000 })
}
export function WireGuardReadiness({ nodeID, role, query, readOnly = false }: { nodeID: number; role?: string; query: ReturnType<typeof useWireGuardReadiness>; readOnly?: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const state = query.data
  const action = useMutation({ mutationFn: (action: 'check' | 'install') => api.post(`/api/admin/settings/nodes/${nodeID}/wireguard-setup`, { action }),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['wireguard-readiness', nodeID] }) }, onError: toast.err })
  const busy = action.isPending || (!!state?.job && !state.job.done_at)
  const labels: Record<string, string> = {
    paired: t('wgSetup.paired'), proxy: t('wgSetup.full'), fresh: t('wgSetup.fresh'), root: t('wgSetup.root'),
    linux: 'Linux', user: t('privateReady.checks.user'), nft: t('privateReady.checks.nft'),
    guard: t('privateReady.checks.guard'), allow: t('privateReady.checks.allow'), capabilities: t('privateReady.checks.capabilities'),
    full: t('wgSetup.full'), shaper: t('wgSetup.shaper'), runtime: t('wgSetup.runtime'), xray: t('wgSetup.xray'), tool_path: t('wgSetup.path'),
    tool_ip: 'ip', tool_wg: 'wg', tool_nft: 'nft', tool_tc: 'tc', tool_sysctl: 'sysctl',
  }
  return <Card withBorder p="sm"><Stack gap="xs">
    <Group justify="space-between"><Text fw={600}>{role ? `${role} · ` : ''}{state?.name ?? t('wgSetup.title')}</Text><Badge color={state?.ready && !query.isError ? 'teal' : 'orange'}>{t(state?.ready && !query.isError ? 'wgSetup.ready' : 'wgSetup.notReady')}</Badge></Group>
    {query.isError && <Alert color="red">{t('wgSetup.loadError')}</Alert>}
    {!state && !query.isError && <Text size="sm">{t('wgSetup.loading')}</Text>}
    {state && <>
      {!state.supported && <Alert color="orange">{t('wgSetup.upgrade')}</Alert>}
      <Group gap="xs">{state.checks.map(c => <Badge key={c.id} color={c.ok ? 'teal' : 'orange'} variant="light">{c.ok ? '✓' : '✕'} {labels[c.id] ?? c.id}</Badge>)}</Group>
      {state.packages.length > 0 && <Text size="sm">{t('wgSetup.packages')}: {state.packages.join(', ')}</Text>}
      {state.fresh && state.supported && !state.ready && !state.installable && <Text size="xs" c="dimmed">{t('wgSetup.manual')}</Text>}
      {state.reported_at > 0 && <Text size="xs" c="dimmed">{t('wgSetup.reported')}: {new Date(state.reported_at * 1000).toLocaleString()}</Text>}
      {busy && <Text size="sm" c="indigo">{t(state.job?.params.action === 'install' ? 'wgSetup.installing' : 'wgSetup.checking')}</Text>}
      {state.job?.done_at && (state.job.error ? <Alert color="orange">{t('wgSetup.jobFailed')}</Alert> : state.job.result?.error_code ? <Alert color="orange">{t(`wgSetup.errors.${state.job.result.error_code}`, { defaultValue: t('wgSetup.jobFailed') })}</Alert> : <Text size="sm" c="teal">{t(state.job.params.action === 'install' ? 'wgSetup.installed' : 'wgSetup.checked')}</Text>)}
    </>}
    <Group>
      <Button size="xs" variant="light" disabled={busy} onClick={() => !readOnly && state?.supported && state.fresh ? action.mutate('check') : void query.refetch()}>{t('wgSetup.recheck')}</Button>
      {!readOnly && state?.installable && <Button size="xs" loading={busy} disabled={!state.fresh || query.isError} onClick={() => modals.openConfirmModal({ title: t('wgSetup.install'), children: <Text>{t('wgSetup.confirm', { packages: state.packages.join(', ') })}</Text>, labels: { confirm: t('wgSetup.install'), cancel: t('common.cancel') }, onConfirm: () => action.mutate('install') })}>{t('wgSetup.install')}</Button>}
    </Group>
  </Stack></Card>
}
