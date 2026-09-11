import { notifications } from '@mantine/notifications'

export const toast = {
  ok: (message: string) => notifications.show({ message, color: 'teal' }),
  err: (e: unknown) => notifications.show({ message: e instanceof Error ? e.message : String(e), color: 'red' }),
}
