import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider, useAuth } from './lib/auth'
import { AppLayout } from './components/AppLayout'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import NodesPage from './pages/NodesPage'
import NodePage from './pages/NodePage'
import EntriesPage from './pages/EntriesPage'
import UsersPage from './pages/UsersPage'
import PlansPage from './pages/PlansPage'
import OrdersPage from './pages/OrdersPage'
import SettingsPage from './pages/SettingsPage'
import SitePage from './pages/SitePage'
import CouponsPage from './pages/CouponsPage'
import TicketsPage from './pages/TicketsPage'
import GiftsPage from './pages/GiftsPage'
import ArticlesPage from './pages/ArticlesPage'
import WithdrawalsPage from './pages/WithdrawalsPage'
import AdminsPage from './pages/AdminsPage'
import { Center, Loader } from '@mantine/core'

function Protected({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  if (loading) return <Center h="100vh"><Loader /></Center>
  if (!me) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<Protected><AppLayout /></Protected>}>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/nodes/:id" element={<NodePage />} />
          <Route path="/entries" element={<EntriesPage />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="/plans" element={<PlansPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/coupons" element={<CouponsPage />} />
          <Route path="/tickets" element={<TicketsPage />} />
          <Route path="/gifts" element={<GiftsPage />} />
          <Route path="/articles" element={<ArticlesPage />} />
          <Route path="/withdrawals" element={<WithdrawalsPage />} />
          <Route path="/admins" element={<AdminsPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/site" element={<SitePage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AuthProvider>
  )
}
