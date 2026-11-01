import { Alert, Stack, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { LineExpectedForward } from '../lib/api'
import { hostPort } from '../lib/ingress'
import { pathBackend, pathNetworkLabel } from '../lib/path-routes'

type Wire = Pick<LineExpectedForward, 'listen' | 'port' | 'protocol' | 'backend'>

export function ForwardTransportSummary({ forwards }: { forwards: Wire[] }) {
  const { t } = useTranslation()
  return <Stack gap={2}>{forwards.map((forward, index) => <Text key={index} size="xs" style={{ overflowWrap: 'anywhere' }}>
    {pathNetworkLabel(forward.protocol)} · {t(`lines.backends.${pathBackend(forward.backend)}`)} · {hostPort(forward.listen || '*', forward.port)}
  </Text>)}</Stack>
}

export function PathTransportWarnings({ forwards }: { forwards: Wire[] }) {
  const { t } = useTranslation()
  const backends = new Set(forwards.map(forward => pathBackend(forward.backend)))
  return <Stack gap="xs">{(['realm', 'nft', 'relay'] as const).filter(backend => backends.has(backend)).map(backend => <Alert key={backend} color="yellow">{t(`pathRoutes.releaseWarnings.${backend}`)}</Alert>)}</Stack>
}
