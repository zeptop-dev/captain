import { Alert, Button, Card, Group, Modal, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { api, runNodeJob } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'
import { serverSettings, type NodeDetail, type ServerSetting } from '../lib/server-detail'
import { NodeDetailsCard } from './NodeDetailsCard'
import { ServerProxySettings } from './ServerProxySettings'
import { NodeModeCard } from './NodeModeCard'
import { CoreManagementCard, type CoreInventory } from './CoreManagementCard'
import { DeleteNodeModal } from './DeleteNodeModal'
import { PairCodeBox } from '../pages/NodesPage'

export function NodeSettings({ detail: d, section }: { detail: NodeDetail; section: ServerSetting }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const n = d.node
  const allowed = n.proxy_allowed !== false
  const current = !allowed ? 'runtime' : section
  return <>
    <Group component="nav" aria-label={t('serverDetail.tabs.settings')} mb="lg" gap="xs">{serverSettings.filter(s => allowed || s === 'runtime').map(s => <Button key={s} component={Link} to={`/nodes/${n.id}/settings/${s}`} variant={current === s ? 'light' : 'subtle'} aria-current={current === s ? 'page' : undefined} onClick={e => { if (current === s) e.preventDefault() }}>{t(`serverDetail.settings.${s}`)}</Button>)}</Group>
    {current === 'runtime' ? <>
      <NodeModeCard node={n} canWrite={me?.role === 'admin'} />
      {allowed && n.paired && me?.role === 'admin' && <CoreManagementCard inventory={d.status?.core_inventory} online={n.online} canWrite inbounds={d.inbounds.filter(ib => ib.Enabled).map(ib => ib.Tag)} onAction={async request => {
        const result = await runNodeJob<CoreInventory>(n.id, 'core_manage', request, 920_000)
        qc.setQueryData<NodeDetail>(['node', String(n.id)], old => old?.status ? { ...old, status: { ...old.status, core_inventory: result } } : old)
        qc.invalidateQueries({ queryKey: ['node', String(n.id)] }); qc.invalidateQueries({ queryKey: ['nodes'] }); qc.invalidateQueries({ queryKey: ['core-options'] })
      }} />}
      <NodeMaintenance detail={d} />
    </> : !n.paired ? <Alert color="orange">{t('nodes.unpaired')}</Alert> : current === 'proxy' ? <NodeDetailsCard node={n} section="policy" /> : <ServerProxySettings node={n} inbounds={d.inbounds} ingresses={d.ingresses ?? []} section={current} singleSection />}
  </>
}

function NodeMaintenance({ detail: { node: n } }: { detail: NodeDetail }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const nav = useNavigate()
  const [pair, setPair] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const repair = useMutation({ mutationFn: () => api.post<{ pair_code: string }>(`/api/admin/nodes/${n.id}/repair`), onSuccess: r => { setPair(r.pair_code); qc.invalidateQueries({ queryKey: ['node'] }); qc.invalidateQueries({ queryKey: ['nodes'] }) }, onError: toast.err })
  return <Card mb="lg"><Stack><Text fw={600}>{t('serverDetail.maintenance')}</Text><Text size="sm" c="dimmed">{t('serverDetail.maintenanceHint')}</Text><Group>
    {me?.role === 'admin' && <Button component={Link} to={`/nodes/${n.id}/host`} variant="light">{t('host.title')}</Button>}
    <Button variant="default" loading={repair.isPending} onClick={() => modals.openConfirmModal({ title: t('nodes.repair'), children: <Text size="sm">{t('nodes.repairHint')}</Text>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => repair.mutate() })}>{t('nodes.repair')}</Button>
    <Button variant="light" color="red" onClick={() => setDeleting(true)}>{t('common.delete')}</Button>
  </Group></Stack>
    <DeleteNodeModal node={n} opened={deleting} onClose={() => setDeleting(false)} onDeleted={() => { qc.invalidateQueries({ queryKey: ['nodes'] }); nav('/nodes') }} />
    <Modal opened={pair !== null} onClose={() => setPair(null)} title={t('nodes.pairTitle')} size="lg">{pair && <PairCodeBox code={pair} mode={n.mode} />}</Modal>
  </Card>
}
