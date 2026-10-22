import { Alert, Badge, Button, Card, Code, Group, Modal, Stack, Switch, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../lib/api'
import { toast } from '../../lib/notify'
import { useSettingsDirty } from '../../lib/settings-draft'
import { SettingsLoadState } from '../SettingsLoadState'

type Client = { id: string; name: string; redirect_uri: string; enabled: boolean }
const path = '/api/admin/settings/login-clients'
export default function LoginClientsCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['login-clients'], queryFn: () => api.get<{ clients: Client[]; base_url: string }>(path) })
  const [draft, setDraft] = useState<Client | null>(null)
  const [secret, setSecret] = useState('')
  const [pendingAction, setPendingAction] = useState<{ client: Client; action: 'rotate' | 'delete' } | null>(null)
  useSettingsDirty(draft !== null || secret !== '')
  const save = useMutation({ mutationFn: (c: Client) => c.id ? api.put<{ client_secret: string }>(`${path}/${c.id}`, c) : api.post<{ client_secret: string }>(path, c), onSuccess: r => { setDraft(null); setSecret(r.client_secret); qc.invalidateQueries({ queryKey: ['login-clients'] }); toast.ok(t('common.saved')) }, onError: toast.err })
  const action = useMutation({ mutationFn: async (v: NonNullable<typeof pendingAction>) => v.action === 'rotate' ? api.post<{ client_secret: string }>(`${path}/${v.client.id}/rotate`) : api.del<{ client_secret?: string }>(`${path}/${v.client.id}`), onSuccess: r => { setPendingAction(null); setSecret(r.client_secret ?? ''); qc.invalidateQueries({ queryKey: ['login-clients'] }) }, onError: toast.err })
  if (!q.data) return <SettingsLoadState query={q} />
  const base = q.data.base_url.replace(/\/$/, '')
  return <Stack>
    <Title order={4}>{t('loginClients.title')}</Title>
    <Alert>{t('loginClients.hint')}</Alert>
    <Card><Stack gap="xs"><Text>{t('loginClients.setup')}</Text>
      <TextInput readOnly label={t('loginClients.authorization')} value={`${base}/api/login-provider/authorize`} />
      <TextInput readOnly label={t('loginClients.token')} value={`${base}/api/login-provider/token`} />
      <TextInput readOnly label={t('loginClients.userinfo')} value={`${base}/api/login-provider/userinfo`} />
      <Text size="sm">{t('loginClients.mapping')}</Text>
    </Stack></Card>
    <Button onClick={() => setDraft({ id: '', name: 'New API', redirect_uri: '', enabled: true })}>{t('loginClients.add')}</Button>
    {q.data.clients.map(c => <Card key={c.id}><Stack gap="xs"><Group justify="space-between"><Text fw={600}>{c.name}</Text><Badge color={c.enabled ? 'teal' : 'gray'}>{t(c.enabled ? 'loginClients.enabled' : 'loginClients.disabled')}</Badge></Group><TextInput label="Client ID" value={c.id} readOnly /><Text size="sm" style={{ overflowWrap: 'anywhere' }}>{c.redirect_uri}</Text><Group><Button variant="light" onClick={() => setDraft(c)}>{t('common.edit')}</Button><Button variant="light" onClick={() => setPendingAction({ client: c, action: 'rotate' })}>{t('loginClients.rotate')}</Button><Button color="red" variant="subtle" onClick={() => setPendingAction({ client: c, action: 'delete' })}>{t('common.delete')}</Button></Group></Stack></Card>)}
    <Modal opened={draft !== null} onClose={() => !save.isPending && setDraft(null)} title={t('loginClients.title')}>
      {draft && <form onSubmit={e => { e.preventDefault(); save.mutate(draft) }}><Stack><TextInput label={t('loginClients.name')} required maxLength={100} value={draft.name} onChange={e => setDraft({ ...draft, name: e.currentTarget.value })} /><TextInput label={t('loginClients.callback')} description={t('loginClients.callbackHint')} placeholder="https://newapi.example.com/oauth/captain" required value={draft.redirect_uri} onChange={e => setDraft({ ...draft, redirect_uri: e.currentTarget.value })} /><Switch label={t('loginClients.enabled')} checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.currentTarget.checked })} /><Text size="sm">{t('loginClients.invalidate')}</Text><Button type="submit" loading={save.isPending}>{t('common.save')}</Button></Stack></form>}
    </Modal>
    <Modal opened={secret !== ''} onClose={() => setSecret('')} title="Client Secret"><Stack><Alert>{t('loginClients.secretHint')}</Alert><Code block style={{ overflowWrap: 'anywhere', whiteSpace: 'pre-wrap' }}>{secret}</Code><Button onClick={() => setSecret('')}>{t('loginClients.dismiss')}</Button></Stack></Modal>
    <Modal opened={pendingAction !== null} onClose={() => !action.isPending && setPendingAction(null)} title={pendingAction?.client.name}><Stack><Text>{t('loginClients.invalidate')}</Text><Button color="red" loading={action.isPending} onClick={() => pendingAction && action.mutate(pendingAction)}>{t(pendingAction?.action === 'rotate' ? 'loginClients.rotate' : 'common.delete')}</Button></Stack></Modal>
  </Stack>
}
