import { Accordion, Alert, Badge, Button, Card, Group, Select, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { coreName } from '../lib/coreSelection'

export interface CorePackage { distribution: string; family: string; version: string; status: string; note?: string; installed: boolean; available: boolean }
export interface CoreInventory { revision: number; packages: CorePackage[]; instances: { distribution: string; version: string; external?: boolean; preparation_error?: string }[] }
export interface CoreRequest { action: 'download' | 'activate'; distribution: string; version: string; revision: number }

export function CoreManagementCard({ inventory, online, canWrite, onAction, inbounds = [] }: {
  inventory?: CoreInventory | null; online: boolean; canWrite: boolean; onAction: (request: CoreRequest) => Promise<void>; inbounds?: string[]
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const execute = async (request: CoreRequest) => {
    setBusy(request.distribution); setError(''); setSuccess('')
    try { await onAction(request); setSuccess(t(request.action === 'download' ? 'coreManager.downloaded' : 'coreManager.activated')) }
    catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy('') }
  }
  return <Card mb="lg" id="node-cores">
    <Accordion variant="default">
      <Accordion.Item value="cores">
        <Accordion.Control><Text fw={600}>{t('coreManager.title')}</Text><Text size="xs" c="dimmed">{t('coreManager.subtitle')}</Text></Accordion.Control>
        <Accordion.Panel>
          <Stack gap="md">
            {!inventory ? <Alert color="gray">{t('coreManager.unavailable')}</Alert> : <>
              {!online && <Alert color="orange">{t('coreManager.offline')}</Alert>}
              {!canWrite && <Text size="sm" c="dimmed">{t('coreManager.readOnly')}</Text>}
              {busy && <Alert color="blue">{t('coreManager.working')}</Alert>}
              {error && <Alert color="red" role="alert">{error}</Alert>}
              {success && <Alert color="teal" role="status">{success}</Alert>}
              {[...new Set(inventory.packages.map(p => p.distribution))].map(name => {
                const packages = inventory.packages.filter(p => p.distribution === name)
                const current = inventory.instances.find(i => i.distribution === name)
                const version = selected[name] ?? current?.version ?? packages[0]?.version
                const pkg = packages.find(p => p.version === version)
                const active = !!current && !current.preparation_error && current.version === version
                const disabled = !online || !canWrite || !!busy || !pkg?.available || pkg.status === 'broken'
                return <Card key={name} withBorder p="md">
                  <Group justify="space-between" wrap="wrap" mb="sm">
                    <Text fw={600}>{coreName(name)}</Text>
                    <Badge color={current?.preparation_error ? 'orange' : current ? 'teal' : 'gray'}>{current?.preparation_error ? t('coreManager.preparing') : current ? `${t('coreManager.active')}: ${current.external ? t('coreManager.external') : current.version}` : t('coreManager.notEnabled')}</Badge>
                  </Group>
                  {current?.preparation_error && <Alert color="orange" role="status" mb="sm">{current.preparation_error}</Alert>}
                  <Select label={t('coreManager.version')} value={version} allowDeselect={false} searchable
                    data={packages.map(p => ({ value: p.version, label: `${p.version} · ${t(`coreManager.${p.status}`)}${p.installed ? ` · ${t('coreManager.installed')}` : ''}`, disabled: p.status === 'broken' || !p.available }))}
                    onChange={v => v && setSelected(old => ({ ...old, [name]: v }))} />
                  {name === 'singbox-extended' && <Text size="xs" c="dimmed" mt="xs">{t('coreManager.extendedHint')}</Text>}
                  {current?.external && <Text size="xs" c="dimmed" mt="xs">{t('coreManager.externalHint')}</Text>}
                  <Group justify="flex-end" mt="md" gap="xs">
                    <Button size="xs" variant="light" disabled={disabled || pkg?.installed} loading={busy === name}
                      onClick={() => void execute({ action: 'download', distribution: name, version, revision: inventory.revision })}>{pkg?.installed ? t('coreManager.installed') : t('coreManager.download')}</Button>
                    <Button size="xs" disabled={disabled || !pkg?.installed || active || !!current?.external}
                      onClick={() => modals.openConfirmModal({ title: t('coreManager.activate'), children: <Stack gap="xs"><Text size="sm">{t('coreManager.activateHint', { core: coreName(name), version })}</Text>{inbounds.length > 0 && <Text size="xs" c="dimmed">{t('coreManager.inbounds')}: {inbounds.join(', ')}</Text>}</Stack>, labels: { confirm: t('coreManager.activate'), cancel: t('common.cancel') }, onConfirm: () => void execute({ action: 'activate', distribution: name, version, revision: inventory.revision }) })}>
                      {active ? t('coreManager.active') : t('coreManager.activate')}
                    </Button>
                  </Group>
                </Card>
              })}
            </>}
          </Stack>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  </Card>
}
