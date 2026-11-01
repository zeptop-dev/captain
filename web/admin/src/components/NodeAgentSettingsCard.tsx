import { Button, Card, Group, Modal, Stack, Text } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Node } from '../lib/api'
import { nodeEditValues } from '../lib/node-edit'
import { toast } from '../lib/notify'
import { useSettingsDirty } from '../lib/settings-draft'
import { AgentConnectionFields } from './AgentConnectionFields'

export function NodeAgentSettingsCard({ node }: { node: Node }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [baseline, setBaseline] = useState({ AgentURL: node.agent_url ?? '', DownloadSource: node.download_source ?? 'github' })
  const form = useForm({ initialValues: baseline })
  useSettingsDirty(editing && form.isDirty())
  const save = useMutation({
    mutationFn: async (values: typeof baseline) => {
      const latest = await api.get<{ node: Node }>(`/api/admin/nodes/${node.id}`)
      const changed = Object.fromEntries(Object.entries(values).filter(([key, value]) => value !== baseline[key as keyof typeof baseline]))
      return api.patch(`/api/admin/nodes/${node.id}`, { ...nodeEditValues(latest.node), ...changed })
    },
    onSuccess: () => { setEditing(false); qc.invalidateQueries({ queryKey: ['node'] }); qc.invalidateQueries({ queryKey: ['nodes'] }); toast.ok(t('common.saved')) },
    onError: toast.err,
  })
  const open = () => {
    const values = { AgentURL: node.agent_url ?? '', DownloadSource: node.download_source ?? 'github' }
    setBaseline(values); form.setValues(values); form.resetDirty(values); setEditing(true)
  }
  const close = () => {
    if (save.isPending) return
    if (!form.isDirty()) { setEditing(false); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: () => setEditing(false) })
  }
  return <>
    <Card mb="lg"><Stack gap="xs">
      <Group justify="space-between"><Text fw={600}>{t('agentConnection.title')}</Text><Button size="xs" variant="light" onClick={open}>{t('common.edit')}</Button></Group>
      <Text size="sm" style={{ overflowWrap: 'anywhere' }}>{node.effective_agent_url || node.agent_url || t('agentConnection.default')}</Text>
      <Text size="sm">{t('agentConnection.source')}: {t(node.download_source === 'captain' ? 'agentConnection.captain' : 'agentConnection.github')}</Text>
      <Text size="sm" c="dimmed">{t('agentConnection.futureOnly')}</Text>
    </Stack></Card>
    <Modal opened={editing} onClose={close} title={t('agentConnection.title')} closeOnClickOutside={false} size="lg">
      <form onSubmit={form.onSubmit(values => save.mutate(values))}><fieldset disabled={save.isPending} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}><Stack>
        <AgentConnectionFields agentURL={form.values.AgentURL} downloadSource={form.values.DownloadSource} onURLChange={value => form.setFieldValue('AgentURL', value)} onSourceChange={value => form.setFieldValue('DownloadSource', value)} />
        <Text size="sm" c="dimmed">{t('agentConnection.futureOnly')}</Text>
        <Group justify="flex-end"><Button variant="default" onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Group>
      </Stack></fieldset></form>
    </Modal>
  </>
}
