import { Group, Paper, SimpleGrid, Text, Title } from '@mantine/core'
import type { ReactNode } from 'react'

// InfoTile is one "label above value" box; InfoGrid lays them out three
// across. SectionTitle heads a group of tiles with an accent icon and
// GroupLabel names a sub-group inside it.
export function InfoTile({ label, value, mono, hint }: { label: ReactNode; value: ReactNode; mono?: boolean; hint?: ReactNode }) {
  return (
    <Paper p="md" radius="md">
      <Text size="xs" c="dimmed" mb={4}>{label}</Text>
      <Text size="sm" fw={600} ff={mono ? 'monospace' : undefined} style={{ wordBreak: 'break-all' }}>{value === undefined || value === null || value === '' ? '—' : value}</Text>
      {hint && <Text size="xs" c="dimmed" mt={2}>{hint}</Text>}
    </Paper>
  )
}

export function InfoGrid({ children, cols = 3 }: { children: ReactNode; cols?: number }) {
  return <SimpleGrid cols={{ base: 1, sm: 2, lg: cols }} spacing="sm">{children}</SimpleGrid>
}

export function SectionTitle({ icon, children, right }: { icon?: ReactNode; children: ReactNode; right?: ReactNode }) {
  return (
    <Group justify="space-between" mb="md">
      <Group gap="xs">
        {icon && <span style={{ color: 'var(--mantine-primary-color-filled)', display: 'inline-flex' }}>{icon}</span>}
        <Title order={4}>{children}</Title>
      </Group>
      {right}
    </Group>
  )
}

export function GroupLabel({ children }: { children: ReactNode }) {
  return <Text size="sm" c="dimmed" fw={500} mt="md" mb="xs">{children}</Text>
}
