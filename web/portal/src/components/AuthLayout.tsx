import { ActionIcon, Anchor, Group, Menu, Text, Title } from '@mantine/core'
import { IconArrowLeft, IconArrowUpRight, IconCheck, IconCube, IconDeviceLaptop, IconLanguage, IconServer } from '@tabler/icons-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useSite } from '../lib/theme'
import { BrandMark } from './BrandMark'
import { ThemeToggle } from './ThemeToggle'
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
  const [year] = useState(() => new Date().getFullYear())
  useEffect(() => { window.scrollTo({ top: 0, behavior: 'instant' }) }, [])
  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <a href="/" className={styles.brand}>
            <BrandMark branding={site.data?.branding} fallbackClassName={styles.brandMark} />
            <span className={styles.brandName}>{brand}</span>
          </a>
          <Group gap={4} wrap="nowrap" className={styles.headerActions}>
            <Anchor href="/" className={styles.homeLink} aria-label={t('auth.home')}><IconArrowLeft size={16} stroke={1.5} aria-hidden /><span>{t('auth.home')}</span></Anchor>
            <ThemeToggle />
            <Menu position="bottom-end" shadow="sm">
              <Menu.Target><ActionIcon size={36} variant="subtle" color="gray" className={styles.language} aria-label={t('portalNav.language')}><IconLanguage size={19} stroke={1.5} aria-hidden /></ActionIcon></Menu.Target>
              <Menu.Dropdown>{LANGUAGES.map(([code, label]) => <Menu.Item key={code} onClick={() => i18n.changeLanguage(code)} rightSection={i18n.resolvedLanguage === code ? <IconCheck size={15} aria-hidden /> : undefined}>{label}</Menu.Item>)}</Menu.Dropdown>
            </Menu>
          </Group>
        </div>
      </header>
      <main className={styles.main}>
        <div className={styles.intro}>
          <span className={styles.portalLabel}>{t('auth.portalLabel')}</span>
          <h1 className={styles.introTitle}>{t('auth.introTitle')}<em>{t('auth.introAccent')}</em></h1>
          <p className={styles.introDescription}>{t('auth.introHint')}</p>
          <div className={styles.connectionArt} aria-hidden>
            <span className={styles.orbit} />
            <span className={`${styles.orbit} ${styles.orbitMiddle}`} />
            <span className={`${styles.orbit} ${styles.orbitInner}`} />
            <span className={styles.hairline} />
            <span className={styles.artTerminal}><IconDeviceLaptop size={34} stroke={1} /></span>
            <span className={styles.artCore}><IconCube size={34} stroke={1.2} /></span>
            <span className={`${styles.artTerminal} ${styles.artEnd}`}><IconServer size={34} stroke={1} /></span>
            <span className={styles.artPoint} />
            <span className={`${styles.artPoint} ${styles.artPointSecond}`} />
            <span className={styles.artCaption}>{t('auth.introCaption')}</span>
          </div>
          <div className={styles.introFoot}><span>{t('auth.introFooter')}</span><IconArrowUpRight size={18} stroke={1.5} aria-hidden /></div>
        </div>
        <section className={styles.card} aria-labelledby="auth-title">
          <div className={styles.cardHeading}>
            <Title id="auth-title" order={2} className={styles.title}>{title}</Title>
            <Text className={styles.description}>{description}</Text>
          </div>
          {children}
          {footer && <div className={styles.footer}>{footer}</div>}
        </section>
      </main>
      <footer className={styles.siteFooter}><span>© {year} {brand}</span><span>{t('auth.portalLabel')}</span></footer>
    </div>
  )
}
