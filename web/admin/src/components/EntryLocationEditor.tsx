import { Button, Group, Modal, Stack, Text } from '@mantine/core'
import { modals } from '@mantine/modals'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type Entry } from '../lib/api'
import { locationDraft, locationDraftError, locationSettings, type GeoLocation, type LocationSettings } from '../lib/location'
import { useSettingsForm } from '../lib/settings-draft'
import { toast } from '../lib/notify'
import { LocationFields } from './LocationFields'

export function EntryLocationEditor({ entry, onClose }: { entry: Entry; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const form = useSettingsForm({ initialValues: { location: locationDraft(entry.LocationSettings) } })
  const save = useMutation({
    mutationFn: () => api.put<{ LocationSettings: LocationSettings; Location: GeoLocation | null }>(`/api/admin/entries/${entry.ID}/location`, locationSettings(form.values.location)),
    onSuccess: () => {
      form.resetDirty(); qc.invalidateQueries({ queryKey: ['entries'] }); qc.invalidateQueries({ queryKey: ['service-status-preview'] }); toast.ok(t('common.saved')); onClose()
    }, onError: toast.err,
  })
  const close = () => {
    if (save.isPending) return
    if (!form.isDirty()) { onClose(); return }
    modals.openConfirmModal({ title: t('workspace.unsaved'), children: <Text size="sm">{t('workspace.leaveHint')}</Text>, labels: { confirm: t('workspace.discard'), cancel: t('workspace.stay') }, onConfirm: onClose })
  }
  return <Modal opened onClose={close} title={`${t('mapLocation.title')} · ${entry.Name}`} size="lg" closeOnClickOutside={false} closeOnEscape={!save.isPending} withCloseButton={!save.isPending}>
    <form onSubmit={event => { event.preventDefault(); if (!save.isPending && !locationDraftError(form.values.location)) save.mutate() }}><Stack>
      <LocationFields value={form.values.location} onChange={value => form.setFieldValue('location', value)} effective={entry.Location} inherited disabled={save.isPending} />
      <Group justify="flex-end"><Button variant="default" disabled={save.isPending} onClick={close}>{t('common.cancel')}</Button><Button type="submit" loading={save.isPending} disabled={!!locationDraftError(form.values.location)}>{t('common.save')}</Button></Group>
    </Stack></form>
  </Modal>
}
