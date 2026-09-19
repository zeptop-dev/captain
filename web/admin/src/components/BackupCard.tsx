import { Alert, Anchor, Button, Card, Code, Group, Modal, NumberInput, PasswordInput, Select, Stack, Switch, Table, Text, TextInput, Title } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconCloudUpload, IconDatabaseExport, IconDownload, IconKey } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { copyText } from '../lib/clipboard'
import { bytes, when } from '../lib/format'
import { toast } from '../lib/notify'

interface Settings { keep: number; hour: number; remote: string; remote_keep: number; webdav: { url: string; username: string; password: string }; s3: { endpoint: string; region: string; bucket: string; prefix: string; access_key: string; secret_key: string; path_style: boolean }; encrypt: { mode: string; recipient: string; passphrase: string } }
interface Status { last_at: string; last_file: string; last_error: string; remote_at: string; remote_name: string; remote_error: string }
interface Resp { available: boolean; settings: Settings; has_webdav_password: boolean; has_s3_secret: boolean; has_encrypt_passphrase: boolean; status: Status; files: { name: string; size: number; mod_time: string }[]; dir: string }

const empty: Settings = { keep: 7, hour: 3, remote: '', remote_keep: 30, webdav: { url: '', username: '', password: '' }, s3: { endpoint: '', region: 'auto', bucket: '', prefix: '', access_key: '', secret_key: '', path_style: false }, encrypt: { mode: '', recipient: '', passphrase: '' } }

// Daily SQLite snapshots kept locally and optionally pushed to WebDAV or an
// S3-compatible bucket. Secrets are write-only.
export function BackupCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['backup'], queryFn: () => api.get<Resp>('/api/admin/settings/backup') })
  const form = useForm<Settings>({ initialValues: empty })
  useEffect(() => { if (q.data?.settings) form.setValues({ ...empty, ...q.data.settings, webdav: { ...empty.webdav, ...q.data.settings.webdav, password: '' }, s3: { ...empty.s3, ...q.data.settings.s3, secret_key: '' }, encrypt: { ...empty.encrypt, ...q.data.settings.encrypt, passphrase: '' } }) }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const refresh = () => qc.invalidateQueries({ queryKey: ['backup'] })
  const save = useMutation({ mutationFn: (v: Settings) => api.put('/api/admin/settings/backup', v), onSuccess: () => { toast.ok(t('common.saved')); refresh() }, onError: toast.err })
  const test = useMutation({ mutationFn: (v: Settings) => api.post('/api/admin/settings/backup/test', v), onSuccess: () => toast.ok(t('backup.testOk')), onError: toast.err })
  const run = useMutation({ mutationFn: () => api.post('/api/admin/settings/backup/run'), onSuccess: () => { toast.ok(t('backup.ran')); refresh() }, onError: (e: Error) => { toast.err(e); refresh() } })
  // A generated identity is shown once and never stored by the panel.
  const [identity, setIdentity] = useState<{ recipient: string; identity: string; created: string } | null>(null)
  const keygen = useMutation({
    mutationFn: () => api.post<{ recipient: string; identity: string }>('/api/admin/settings/backup/keygen'),
    onSuccess: (r) => { form.setFieldValue('encrypt.recipient', r.recipient); setIdentity({ ...r, created: new Date().toISOString() }) }, onError: toast.err,
  })
  const keyFile = identity ? `# Captain backup key, created ${identity.created}\n# public key: ${identity.recipient}\n${identity.identity}\n` : ''
  if (q.data && !q.data.available) return null
  const st = q.data?.status
  const v = form.values
  return (
    <Card>
      <Title order={5} mb="xs">{t('backup.title')}</Title>
      <Text size="xs" c="dimmed" mb="sm">{t('backup.hint', { dir: q.data?.dir ?? '' })}</Text>
      <form onSubmit={form.onSubmit((x) => save.mutate(x))}><Stack gap="sm">
        <Group grow align="flex-end">
          <NumberInput label={t('backup.hour')} min={0} max={23} {...form.getInputProps('hour')} />
          <NumberInput label={t('backup.keep')} min={1} max={365} {...form.getInputProps('keep')} />
          <Select label={t('backup.remote')} data={[{ value: '', label: t('backup.remoteNone') }, { value: 'webdav', label: 'WebDAV' }, { value: 's3', label: 'S3 / R2 / B2 / MinIO' }]} value={v.remote} onChange={(x) => form.setFieldValue('remote', x ?? '')} allowDeselect={false} />
          {v.remote && <NumberInput label={t('backup.remoteKeep')} description={t('backup.remoteKeepHint')} min={0} {...form.getInputProps('remote_keep')} />}
        </Group>
        {v.remote === 'webdav' && <Group grow align="flex-end">
          <TextInput label="URL" placeholder="https://dav.example.com/remote.php/dav/files/me/captain/" {...form.getInputProps('webdav.url')} />
          <TextInput label={t('backup.username')} {...form.getInputProps('webdav.username')} />
          <PasswordInput label={t('backup.password')} placeholder={q.data?.has_webdav_password ? t('backup.keepSecret') : ''} {...form.getInputProps('webdav.password')} />
        </Group>}
        {v.remote === 's3' && <>
          <Group grow align="flex-end">
            <TextInput label="Endpoint" placeholder="https://<account>.r2.cloudflarestorage.com" {...form.getInputProps('s3.endpoint')} />
            <TextInput label="Region" placeholder="auto" {...form.getInputProps('s3.region')} />
            <TextInput label="Bucket" {...form.getInputProps('s3.bucket')} />
            <TextInput label={t('backup.prefix')} placeholder="captain" {...form.getInputProps('s3.prefix')} />
          </Group>
          <Group grow align="flex-end">
            <TextInput label="Access key" {...form.getInputProps('s3.access_key')} />
            <PasswordInput label="Secret key" placeholder={q.data?.has_s3_secret ? t('backup.keepSecret') : ''} {...form.getInputProps('s3.secret_key')} />
            <Switch label={t('backup.pathStyle')} description={t('backup.pathStyleHint')} {...form.getInputProps('s3.path_style', { type: 'checkbox' })} />
          </Group>
        </>}
        {v.remote && <Stack gap={6}>
          <Select label={t('backup.encrypt')} description={t('backup.encryptHint')} allowDeselect={false} value={v.encrypt.mode} onChange={(x) => form.setFieldValue('encrypt.mode', x ?? '')}
            data={[{ value: '', label: t('backup.encryptOff') }, { value: 'key', label: t('backup.encryptKey') }, { value: 'passphrase', label: t('backup.encryptPassphrase') }]} />
          {v.encrypt.mode === '' && <Text size="xs" c="orange">{t('backup.encryptOffWarn')}</Text>}
          {v.encrypt.mode === 'key' && <Group align="flex-end" wrap="nowrap">
            <TextInput style={{ flex: 1 }} label={t('backup.recipient')} placeholder="age1…" {...form.getInputProps('encrypt.recipient')} />
            <Button variant="light" leftSection={<IconKey size={14} />} loading={keygen.isPending} onClick={() => keygen.mutate()}>{t('backup.generateKey')}</Button>
          </Group>}
          {v.encrypt.mode === 'passphrase' && <PasswordInput label={t('backup.passphrase')} description={t('backup.passphraseHint')} placeholder={q.data?.has_encrypt_passphrase ? t('backup.keepSecret') : ''} {...form.getInputProps('encrypt.passphrase')} />}
          {v.encrypt.mode !== '' && <Text size="xs" c="dimmed">{t('backup.restoreHint')}</Text>}
        </Stack>}
        <Group justify="space-between">
          <Group gap="xs">
            <Button size="xs" variant="light" leftSection={<IconDatabaseExport size={14} />} loading={run.isPending} onClick={() => run.mutate()}>{t('backup.runNow')}</Button>
            {v.remote && <Button size="xs" variant="default" leftSection={<IconCloudUpload size={14} />} loading={test.isPending} onClick={() => test.mutate(v)}>{t('backup.test')}</Button>}
          </Group>
          <Button type="submit" size="xs" loading={save.isPending}>{t('common.save')}</Button>
        </Group>
        {st?.last_at && !st.last_at.startsWith('0001') && (
          <Text size="xs" c={st.last_error || st.remote_error ? 'red' : 'dimmed'}>
            {t('backup.last', { at: when(st.last_at), file: st.last_file })}{st.last_error ? ` — ${st.last_error}` : ''}
            {st.remote_name && !st.remote_error ? ` · ${t('backup.uploaded', { name: st.remote_name })}` : ''}{st.remote_error ? ` · ${t('backup.uploadFailed')}: ${st.remote_error}` : ''}
          </Text>
        )}
        {(q.data?.files?.length ?? 0) > 0 && (
          <Table fz="xs"><Table.Tbody>
            {q.data!.files.slice(0, 10).map((f) => <Table.Tr key={f.name}><Table.Td><Anchor size="xs" href={`/api/admin/settings/backup/files/${f.name}`} download>{f.name}</Anchor></Table.Td><Table.Td>{bytes(f.size)}</Table.Td><Table.Td c="dimmed">{when(f.mod_time)}</Table.Td></Table.Tr>)}
          </Table.Tbody></Table>
        )}
      </Stack></form>
      <Modal opened={identity !== null} onClose={() => {}} withCloseButton={false} closeOnClickOutside={false} closeOnEscape={false} title={t('backup.keyTitle')} size="lg">
        <Stack gap="sm">
          <Alert color="orange">{t('backup.keyWarn')}</Alert>
          <Code block>{keyFile}</Code>
          <Group justify="space-between">
            <Group gap="xs">
              <Button variant="light" leftSection={<IconDownload size={14} />} component="a" href={`data:text/plain;charset=utf-8,${encodeURIComponent(keyFile)}`} download="captain-backup-key.txt">{t('backup.downloadKey')}</Button>
              <Button variant="default" onClick={() => copyText(keyFile).then((ok) => ok && toast.ok(t('common.copied')))}>{t('backup.copyKey')}</Button>
            </Group>
            <Button onClick={() => setIdentity(null)}>{t('backup.keySaved')}</Button>
          </Group>
        </Stack>
      </Modal>
    </Card>
  )
}
