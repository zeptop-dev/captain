import { ActionIcon, Anchor, Card, Group, Menu, Text, Title } from '@mantine/core'
import { IconArrowLeft, IconCheck, IconLanguage } from '@tabler/icons-react'
import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useSite } from '../lib/theme'
import { BrandMark } from './BrandMark'
import styles from './AuthLayout.module.css'

const LANGUAGES: [string, string][] = [['zh-CN', '中文'], ['zh-TW', '繁體中文'], ['en', 'English'], ['ja', '日本語'], ['ko', '한국어'], ['ru', 'Русский']]

export function AuthLayout({ title, description, children, footer }: {
  title: string
  description: string
  children: ReactNode
  footer?: ReactNode
}) {
  const { t, i18n } = useTranslation()
  const site = useSite()
  const brand = site.data?.theme?.portal_title || site.data?.name || 'Captain'
  useEffect(() => { window.scrollTo({ top: 0, behavior: 'instant' }) }, [])
  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <a href="/" className={styles.brand}>
          <BrandMark branding={site.data?.branding} fallbackClassName={styles.brandMark} />
          <span className={styles.brandName}>{brand}</span>
        </a>
        <Group gap="sm" wrap="nowrap" className={styles.headerActions}>
          <Anchor href="/" className={styles.homeLink} aria-label={t('auth.home')}><IconArrowLeft size={16} aria-hidden /><span>{t('auth.home')}</span></Anchor>
          <Menu position="bottom-end" shadow="md">
            <Menu.Target><ActionIcon size="lg" variant="subtle" color="gray" aria-label={t('portalNav.language')}><IconLanguage size={21} /></ActionIcon></Menu.Target>
            <Menu.Dropdown>{LANGUAGES.map(([code, label]) => <Menu.Item key={code} onClick={() => i18n.changeLanguage(code)} rightSection={i18n.resolvedLanguage === code ? <IconCheck size={15} aria-hidden /> : undefined}>{label}</Menu.Item>)}</Menu.Dropdown>
          </Menu>
        </Group>
      </header>
      <main className={styles.main}>
        <div className={styles.intro}>
          <span className={styles.portalLabel}>{t('auth.portalLabel')}</span>
        </div>
        <Card className={styles.card} component="section" aria-labelledby="auth-title">
          <div className={styles.cardHeading}>
            <Title id="auth-title" order={1} className={styles.title}>{title}</Title>
            <Text className={styles.description}>{description}</Text>
          </div>
          {children}
        </Card>
        {footer && <div className={styles.footer}>{footer}</div>}
      </main>
    </div>
  )
}
