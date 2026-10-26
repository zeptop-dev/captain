import { useComputedColorScheme } from '@mantine/core'
import { IconSailboat } from '@tabler/icons-react'
import { useState } from 'react'
import { brandLogo, type Branding } from '../../../shared/branding'
import styles from './BrandMark.module.css'

export function BrandMark({ branding, fallbackClassName }: { branding?: Branding; fallbackClassName?: string }) {
  const scheme = useComputedColorScheme('light')
  const src = brandLogo(branding, scheme)
  const [failed, setFailed] = useState('')
  if (src && failed !== src) return <img className={styles.logo} src={src} alt="" aria-hidden="true" onError={() => setFailed(src)} />
  return <span className={fallbackClassName}><IconSailboat size={25} stroke={1.7} aria-hidden /></span>
}
