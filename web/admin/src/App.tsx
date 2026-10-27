import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/auth'
import { AppLayout } from './components/AppLayout'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import MonitoringPage from './pages/MonitoringPage'
import NodesPage from './pages/NodesPage'
import NodePage from './pages/NodePage'
import HostManagementPage from './pages/HostManagementPage'
import EntriesPage from './pages/EntriesPage'
import UsersPage from './pages/UsersPage'
import PlansPage from './pages/PlansPage'
import OrdersPage from './pages/OrdersPage'
import InfrastructurePage from './pages/InfrastructurePage'
import AccountPage from './pages/AccountPage'
import SettingsPage from './pages/SettingsPage'
import BusinessPage from './pages/BusinessPage'
import { canVisit } from './lib/navigation'
import { BrandingMetadata } from './components/BrandingMetadata'
import SubTemplatesPage from './pages/SubTemplatesPage'
import DomainsPage from './pages/DomainsPage'
import CouponsPage from './pages/CouponsPage'
import TicketsPage from './pages/TicketsPage'
import GiftsPage from './pages/GiftsPage'
import ArticlesPage from './pages/ArticlesPage'
import WithdrawalsPage from './pages/WithdrawalsPage'
import AdminsPage from './pages/AdminsPage'
import ExternalPage from './pages/ExternalPage'
import SpeedtestPage from './pages/SpeedtestPage'
import { Center, Loader } from '@mantine/core'

function Protected({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  const location = useLocation()
  if (loading) return <Center h="100vh"><Loader /></Center>
  if (!me) return <Navigate to="/login" replace />
  if (!canVisit(location.pathname + location.search, me.role)) return <Navigate to="/" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <AuthProvider>
      <BrandingMetadata />
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<Protected><AppLayout /></Protected>}>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/monitoring" element={<MonitoringPage />} />
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:id/*" element={<NodePage />} />
          <Route path="/nodes/:id/host" element={<HostManagementPage />} />
          <Route path="/entries" element={<EntriesPage />} />
          <Route path="/external" element={<ExternalPage />} />
          <Route path="/speedtest" element={<SpeedtestPage />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="/plans" element={<PlansPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/coupons" element={<CouponsPage />} />
          <Route path="/tickets" element={<TicketsPage />} />
          <Route path="/gifts" element={<GiftsPage />} />
          <Route path="/articles" element={<ArticlesPage />} />
          <Route path="/withdrawals" element={<WithdrawalsPage />} />
          <Route path="/admins" element={<AdminsPage />} />
          <Route path="/infrastructure" element={<InfrastructurePage />} />
          <Route path="/account" element={<AccountPage />} />
          <Route path="/settings/*" element={<SettingsPage />} />
          <Route path="/business/:section" element={<BusinessPage />} />
          <Route path="/site" element={<Navigate to="/settings/category/site#site" replace />} />
          <Route path="/sub-templates" element={<SubTemplatesPage />} />
          <Route path="/domains" element={<DomainsPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AuthProvider>
  )
}
