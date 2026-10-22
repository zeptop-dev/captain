import { Alert, Button, Stack, Text } from '@mantine/core'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { runNodeJob } from '../lib/api'
import { useAuth } from '../lib/auth'
import type { DiagnosticResult } from '../lib/diagnostics'
import { DiagnosticResultView } from './NetworkDiagnostic'

interface Props {
  sourceNodeID: number
  sourceNodeName?: string
  target: string
  protocol: string
  disabled?: boolean
}

// Each check belongs to one exact edge. Replacing its source, target or
// transport unmounts the old poller, including a still-pending job submission.
export function ForwardConnectivityCheck(props: Props) {
  const { me } = useAuth()
  if (me?.role !== 'admin') return null
  return <ConnectionCheck key={JSON.stringify([props.sourceNodeID, props.target, props.protocol, !!props.disabled])} {...props} />
}

function ConnectionCheck({ sourceNodeID, sourceNodeName, target, protocol, disabled }: Props) {
  const { t } = useTranslation()
  const request = useRef<AbortController | null>(null)
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<DiagnosticResult | null>(null)
  const [error, setError] = useState('')
  useEffect(() => () => request.current?.abort(), [])

  const run = async () => {
    if (request.current || disabled || !sourceNodeID || !target || protocol === 'udp') return
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setResult(null)
    setError('')
    try {
      const report = await runNodeJob<DiagnosticResult>(sourceNodeID, 'network_diagnostic', { type: 'tcp', target }, 150_000, controller.signal)
      if (controller.signal.aborted || request.current !== controller) return
      if (!report || report.type !== 'tcp' || !report.outcome) throw new Error(t('diagnostics.outcomes.invalid_report'))
      setResult(report)
    } catch (err) {
      if (!controller.signal.aborted && request.current === controller) setError(err instanceof Error ? err.message : t('diagnostics.outcomes.failed'))
    } finally {
      if (!controller.signal.aborted && request.current === controller) {
        request.current = null
        setPending(false)
      }
    }
  }

  if (protocol === 'udp') return <Text size="xs" c="dimmed">{t('forwardConnectionCheck.udpHint')}</Text>
  return <Stack gap={4}>
    <Button type="button" variant="subtle" size="xs" loading={pending} disabled={disabled || !sourceNodeID || !target} onClick={run} style={{ alignSelf: 'flex-start', maxWidth: '100%' }} styles={{ label: { whiteSpace: 'normal', overflowWrap: 'anywhere' }, root: { height: 'auto', minHeight: 30 } }}>
      {t('forwardConnectionCheck.run', { source: sourceNodeName || `#${sourceNodeID}` })}
    </Button>
    <Text size="xs" c="dimmed">{t('forwardConnectionCheck.hint')}</Text>
    {pending && <Text size="xs" c="dimmed">{t('diagnostics.pending')}</Text>}
    {error && <Alert color="orange">{error}</Alert>}
    {result && <DiagnosticResultView result={result} />}
  </Stack>
}
