import { Anchor, Checkbox } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import type { RegistrationPolicy } from '../lib/registration'

export function RegistrationConsent({ policy, checked, onChange }: { policy?: RegistrationPolicy; checked: boolean; onChange: (checked: boolean) => void }) {
  const { t } = useTranslation()
  if (!policy?.legal_version) return null
  const legal = policy.legal!
  return <Checkbox checked={checked} onChange={e => onChange(e.currentTarget.checked)} label={<>{t('auth.agreePrefix')}{' '}{legal.terms_url && <Anchor href={legal.terms_url} target="_blank" rel="noopener noreferrer" onClick={e => e.stopPropagation()}>{t('auth.terms')}</Anchor>}{legal.terms_url && legal.privacy_url && ` ${t('auth.and')} `}{legal.privacy_url && <Anchor href={legal.privacy_url} target="_blank" rel="noopener noreferrer" onClick={e => e.stopPropagation()}>{t('auth.privacy')}</Anchor>}</>} />
}
