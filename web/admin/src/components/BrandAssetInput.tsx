import { Button, FileButton, Group, Stack, Text, TextInput } from '@mantine/core'
import { useMutation } from '@tanstack/react-query'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { safeBrandAssetURL } from '../../../shared/branding'
import { ApiError } from '../lib/api'
import { toast } from '../lib/notify'

export function BrandAssetInput({ label, value, onChange, dark = false }: {
  label: string; value: string; onChange: (value: string) => void; dark?: boolean
}) {
  const { t } = useTranslation()
  const reset = useRef<() => void>(null)
  const upload = useMutation({
    mutationFn: async (file: File) => {
      if (file.size > 2 * 1024 * 1024) throw new Error(t('branding.uploadTooLarge'))
      const res = await fetch('/api/admin/settings/branding/assets', {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': file.type || 'application/octet-stream' }, body: file,
      })
      const body = await res.text()
      let result: { url?: string; error?: string } = {}
      try { result = JSON.parse(body) } catch { /* A reverse proxy may return a plain error page. */ }
      if (!res.ok) throw new ApiError(res.status, result.error || t('branding.uploadFailed'))
      if (!result.url || !safeBrandAssetURL(result.url)) throw new Error(t('branding.uploadFailed'))
      return result.url
    },
    onSuccess: onChange,
    onError: toast.err,
    onSettled: () => reset.current?.(),
  })
  const preview = safeBrandAssetURL(value)
  return <Stack gap="xs" style={{ minWidth: 0 }}>
    <TextInput label={label} placeholder="https://example.com/logo.png" value={value} onChange={event => onChange(event.currentTarget.value)} />
    <Group gap="xs" align="center">
      <div style={{ height: 54, width: 96, flexShrink: 0, display: 'grid', placeItems: 'center', border: '1px solid var(--mantine-color-default-border)', borderRadius: 6, background: dark ? '#17191e' : '#fff', overflow: 'hidden' }}>
        {preview ? <img key={preview} src={preview} alt={label} style={{ maxWidth: 84, maxHeight: 42, objectFit: 'contain' }} /> : <Text c="dimmed" size="xs">{t('branding.noImage')}</Text>}
      </div>
      <FileButton resetRef={reset} onChange={file => { if (file) upload.mutate(file) }} accept="image/png,image/jpeg,image/webp">{props => <Button {...props} size="xs" variant="light" loading={upload.isPending}>{t('branding.upload')}</Button>}</FileButton>
      {value && <Button variant="subtle" size="xs" color="gray" onClick={() => onChange('')}>{t('branding.clear')}</Button>}
    </Group>
  </Stack>
}
