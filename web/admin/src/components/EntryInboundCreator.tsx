import { Alert, Box, Button, Group, Loader, Modal, Select, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type Group as UserGroup, type Inbound, type Ingress, type Node } from '../lib/api'
import { type EntryNodeDetail, type InboundChoice, proxyServer } from '../lib/entry-flow'
import { dnsToast, type DNSResult } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'
import { InboundForm, toPayload, toValues, type InboundValues } from './InboundForm'
import { ingressPayload } from './IngressFields'

// This step creates an actual listener. Its result is returned immediately so
// a later Entry/Line save failure never repeats the successful creation.
export function EntryInboundCreator({ nodes, initialServerID, onCreated, onClose }: {
  nodes: Node[]; initialServerID?: number; onCreated: (choice: InboundChoice) => void; onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [serverID, setServerID] = useState(initialServerID ? String(initialServerID) : '')
  const [dirty, setDirty] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const [hasCreatedIngress, setHasCreatedIngress] = useState(false)
  const createdIngress = useRef<{ fingerprint: string; ingress: Ingress } | null>(null)
  const q = useQuery({ queryKey: ['node', serverID], queryFn: () => api.get<EntryNodeDetail>(`/api/admin/nodes/${serverID}`), enabled: !!serverID })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UserGroup[]>('/api/admin/groups') })
  const save = useMutation({
    mutationFn: async (values: InboundValues) => {
      const node = q.data?.node
      if (!node || !proxyServer(node) || uncertain) throw new Error(t('entryFlow.reloadBeforeRetry'))
      let v = values
      let ingress = q.data?.ingresses?.find(g => String(g.id) === v.IngressID)
      if (v.NewIngress) {
        const fingerprint = JSON.stringify(ingressPayload(v.NewIngress))
        if (createdIngress.current && createdIngress.current.fingerprint !== fingerprint) throw new Error(t('entryFlow.ingressAlreadyCreated'))
        if (!createdIngress.current) {
          try {
            const r = await api.post<{ ingress: Ingress; dns?: DNSResult[] }>(`/api/admin/nodes/${serverID}/ingresses`, ingressPayload(v.NewIngress))
            createdIngress.current = { fingerprint, ingress: r.ingress }
            setHasCreatedIngress(true)
            dnsToast(r.dns)
          } catch (error) { if (!(error instanceof ApiError)) setUncertain(true); throw error }
        }
        ingress = createdIngress.current.ingress
        v = { ...v, IngressID: String(ingress.id), NewIngress: undefined }
      }
      try {
        const ib = await api.post<Inbound>(`/api/admin/nodes/${serverID}/inbounds`, toPayload(v))
        return { ib, node, ingress }
      } catch (error) { if (!(error instanceof ApiError)) setUncertain(true); throw error }
    },
    onSuccess: (choice) => {
      qc.invalidateQueries({ queryKey: ['node', serverID] }); qc.invalidateQueries({ queryKey: ['nodes'] })
      setDirty(false); onCreated(choice)
    },
  })
  useSettingsDirty(dirty || save.isPending)
  const close = () => {
    if (save.isPending) return
    if (!dirty) { onClose(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{createdIngress.current || uncertain ? t('entryFlow.createdResourcesRemain') : t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose })
  }
  const selectServer = (value: string | null) => {
    if (value === serverID || save.isPending) return
    const apply = () => { setServerID(value ?? ''); setDirty(false); createdIngress.current = null; setHasCreatedIngress(false); setUncertain(false); save.reset() }
    if (dirty) modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: apply })
    else apply()
  }
  return <Modal opened onClose={close} closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending} title={t('entryFlow.newInbound')} size="xl">
    <Stack>
      <Alert color="blue">{t('entryFlow.newInboundHint')}</Alert>
      <Select label={t('entryFlow.server')} searchable required allowDeselect={false} value={serverID || null} onChange={selectServer} disabled={save.isPending || hasCreatedIngress || uncertain} data={nodes.filter(proxyServer).map(n => ({ value: String(n.id), label: n.name }))} />
      {(q.isLoading || groups.isLoading) && <Loader size="sm" />}
      {(q.isError || groups.isError) && <Alert color="red"><Text size="sm">{t('entryFlow.loadError')}</Text><Button variant="subtle" size="xs" onClick={() => { q.refetch(); groups.refetch() }}>{t('entryFlow.retry')}</Button></Alert>}
      {save.isError && <Alert color="red">{save.error.message}</Alert>}
      {hasCreatedIngress && <Alert color="blue">{t('entryFlow.ingressSaved')}</Alert>}
      {uncertain && <Alert color="yellow">{t('entryFlow.uncertainCreate')}</Alert>}
      {q.data && groups.data && <Box component="fieldset" disabled={save.isPending || uncertain} style={{ margin: 0, padding: 0, border: 0, minWidth: 0 }} onChangeCapture={() => setDirty(true)}>
        <InboundForm key={serverID} initial={toValues()} nodeID={q.data.node.id} domain={q.data.node.domain} decoyDomain={q.data.node.decoy_enabled ? q.data.node.domain : undefined} lineOnly={!q.data.node.public_addr && !q.data.node.domain} ingresses={q.data.ingresses ?? []} usedPorts={q.data.inbounds.map(i => i.Port)} groups={groups.data} busy={save.isPending} onDirtyChange={setDirty} onSubmit={v => { setDirty(true); save.mutate(v) }} onCancel={close} />
      </Box>}
      {uncertain && <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button></Group>}
    </Stack>
  </Modal>
}
