import { ActionIcon, Tooltip } from '@mantine/core'
import { IconCheck, IconCopy } from '@tabler/icons-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { copyText } from '../lib/clipboard'

// Copy icon that works over plain HTTP too (see lib/clipboard).
export function Copy({ value }: { value: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const onClick = async () => {
    if (await copyText(value)) { setCopied(true); window.clearTimeout(timer.current); timer.current = window.setTimeout(() => setCopied(false), 1500) }
  }
  return (
    <Tooltip label={copied ? t('common.copied') : t('common.copy')} withArrow>
      <ActionIcon variant="subtle" color={copied ? 'teal' : 'gray'} onClick={onClick} size="sm">
        {copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
      </ActionIcon>
    </Tooltip>
  )
}
