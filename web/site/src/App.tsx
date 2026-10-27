import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { SiteLayout } from './components/SiteLayout'
import { getJSON, type Site } from './lib/site'
import { portalEntryURL } from './lib/site-navigation'
import HomePage from './pages/HomePage'
import DownloadsPage from './pages/DownloadsPage'
import ServiceStatusPage from './pages/ServiceStatusPage'

export default function App() {
  const { t } = useTranslation()
  const site = useQuery({ queryKey: ['site'], queryFn: () => getJSON<Site>('/api/site') })
  const path = window.location.pathname.replace(/\/+$/, '')
  const redirectHome = path !== '/service-status' && path !== '/downloads' && site.data?.homepage_enabled === false
  useEffect(() => {
    if (redirectHome) window.location.replace(portalEntryURL(window.location.search))
  }, [redirectHome])
  if (redirectHome) return null
  if (path === '/service-status') return <ServiceStatusPage site={site.data} />
  return <SiteLayout site={site.data}>
    {path === '/downloads' ? <DownloadsPage site={site.data} /> : site.data ? <HomePage site={site.data} /> : <div className="site-container" style={{ minHeight: '60vh', paddingBlock: 100 }}>
      <p role="status">{site.isError ? t('common.loadError') : t('common.loading')}</p>
      {site.isError && <button type="button" onClick={() => void site.refetch()}>{t('common.reload')}</button>}
    </div>}
  </SiteLayout>
}
