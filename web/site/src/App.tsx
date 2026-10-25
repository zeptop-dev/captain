import { useQuery } from '@tanstack/react-query'
import { SiteLayout } from './components/SiteLayout'
import { getJSON, type Site } from './lib/site'
import HomePage from './pages/HomePage'
import ServiceStatusPage from './pages/ServiceStatusPage'

export default function App() {
  const site = useQuery({ queryKey: ['site'], queryFn: () => getJSON<Site>('/api/site') })
  if (window.location.pathname.replace(/\/+$/, '') === '/service-status') return <ServiceStatusPage site={site.data} />
  return <SiteLayout site={site.data}>
    {site.data ? <HomePage site={site.data} /> : <div className="site-container" style={{ minHeight: '60vh', paddingBlock: 100 }}>
      <p role="status">{site.isError ? '暂时无法加载服务信息。' : '正在加载服务信息…'}</p>
      {site.isError && <button type="button" onClick={() => void site.refetch()}>重新加载</button>}
    </div>}
  </SiteLayout>
}
