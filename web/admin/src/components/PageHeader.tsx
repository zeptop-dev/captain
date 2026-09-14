import { Group, Stack, Text, Title } from '@mantine/core'
import type { ReactNode } from 'react'

// Every page opens with a name and one sentence saying what the object is.
export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: string; actions?: ReactNode }) {
  return (
    <Group justify="space-between" align="flex-start" mb="lg" wrap="wrap">
      <Stack gap={4}>
        <Title order={3}>{title}</Title>
        {subtitle && <Text c="dimmed" size="sm" maw={640}>{subtitle}</Text>}
      </Stack>
      {actions && <Group gap="sm">{actions}</Group>}
    </Group>
  )
}
