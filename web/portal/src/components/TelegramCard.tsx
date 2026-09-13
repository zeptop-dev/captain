import { Anchor, Button, Card, Code, Group, Text } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { toast } from '../lib/notify'
import { Copy } from './Copy'

interface TG { enabled: boolean; bot?: string; bound?: boolean; code?: string; link?: string }

// Telegram binding: shows the one-time code (and a t.me deep link) until the
// user has sent it to the bot.
export function TelegramCard() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['telegram'], queryFn: () => api.get<TG>('/api/portal/telegram'), refetchInterval: (r) => (r.state.data?.enabled && !r.state.data?.bound ? 5000 : false) })
  const unbind = useMutation({ mutationFn: () => api.post('/api/portal/telegram/unbind'), onSuccess: () => { toast.ok(t('telegram.unbound')); qc.invalidateQueries({ queryKey: ['telegram'] }) }, onError: toast.err })
  const d = q.data
  if (!d?.enabled) return null
  return (
    <Card>
      <Group justify="space-between" align="flex-start">
        <div>
          <Text size="xs" tt="uppercase" c="dimmed" fw={700}>Telegram</Text>
          {d.bound
            ? <Text size="sm" mt={4}>{t('telegram.bound', { bot: d.bot })}</Text>
            : <Text size="sm" c="dimmed" mt={4}>{t('telegram.hint', { bot: d.bot })}</Text>}
        </div>
        {d.bound && <Button size="xs" variant="subtle" color="red" onClick={() => unbind.mutate()}>{t('telegram.unbind')}</Button>}
      </Group>
      {!d.bound && d.code && (
        <Group mt="sm" gap="xs">
          <Code fz="md" px="sm" py={4}>/bind {d.code}</Code><Copy value={`/bind ${d.code}`} />
          {d.link && <Anchor href={d.link} target="_blank" rel="noreferrer" size="sm">{t('telegram.open')}</Anchor>}
        </Group>
      )}
    </Card>
  )
}
