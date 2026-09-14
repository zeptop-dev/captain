import { Card, Group, Text } from '@mantine/core'
import type { ReactNode } from 'react'

export function Stat({ label, value, hint, icon, color }: { label: string; value: ReactNode; hint?: ReactNode; icon?: ReactNode; color?: string }) {
  return (
    <Card>
      <Group justify="space-between" mb={6}>
        <Text size="sm" c="dimmed" fw={500}>{label}</Text>
        {icon}
      </Group>
      <Text fz={28} fw={700} lh={1.15} c={color}>{value}</Text>
      {hint && <Text size="xs" c="dimmed" mt={6}>{hint}</Text>}
    </Card>
  )
}
