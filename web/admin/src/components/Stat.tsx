import { Card, Group, Text } from '@mantine/core'
import type { ReactNode } from 'react'

export function Stat({ label, value, hint, icon, color }: { label: string; value: ReactNode; hint?: ReactNode; icon?: ReactNode; color?: string }) {
  return (
    <Card>
      <Group justify="space-between" mb="xs">
        <Text size="xs" tt="uppercase" c="dimmed" fw={600} style={{ letterSpacing: '0.08em' }}>{label}</Text>
        {icon}
      </Group>
      <Text fz={32} fw={700} lh={1.1} c={color}>{value}</Text>
      {hint && <Text size="xs" c="dimmed" mt={6}>{hint}</Text>}
    </Card>
  )
}
