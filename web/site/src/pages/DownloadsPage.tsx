import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ClientCatalog } from '../components/ClientCatalog'
import { getJSON, type Site } from '../lib/site'
import type { PublishedClient } from '../../../shared/client-downloads'
import styles from './DownloadsPage.module.css'

export default function DownloadsPage({ site }: { site?: Site }) {
  const { t } = useTranslation()
  const catalog = useQuery({ queryKey: ['client-catalog', 'site'], queryFn: () => getJSON<PublishedClient[]>('/api/site/clients') })
  return <div className={`site-container ${styles.page}`}>
    <header><p className={styles.eyebrow}>{t('nav.support')}</p><h1>{t('clientCatalog.title')}</h1><p>{t('clientCatalog.description')}</p></header>
    {catalog.isPending && <p role="status">{t('common.loading')}</p>}
    {catalog.isError && <div role="alert"><p>{t('common.loadError')}</p><button type="button" onClick={() => void catalog.refetch()}>{t('common.reload')}</button></div>}
    {catalog.data && <ClientCatalog clients={catalog.data} />}
    <div className={styles.links}><a href="/portal/connect">{t('home.actions.guide')} ↗</a>{site?.links.download && <a href={site.links.download}>{t('clientCatalog.moreDownloads')} ↗</a>}</div>
  </div>
}
