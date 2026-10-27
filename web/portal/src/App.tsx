import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Center, Loader } from '@mantine/core'
import { referralCode, withReferral } from './lib/referral'
import { AuthProvider, useAuth } from './lib/auth'
import { Layout } from './components/Layout'
import AuthorizePage from './pages/AuthorizePage'
import AuthPage from './pages/AuthPage'
import ForgotPage from './pages/ForgotPage'
import HomePage from './pages/HomePage'
import PlansPage from './pages/PlansPage'
import OrdersPage from './pages/OrdersPage'
import ServersPage from './pages/ServersPage'
import InvitePage from './pages/InvitePage'
import TicketsPage from './pages/TicketsPage'
import HelpPage from './pages/HelpPage'
import ConnectionPage from './pages/ConnectionPage'
import AccountPage from './pages/AccountPage'
import ServiceStatusPage from './pages/ServiceStatusPage'

function Protected({ children }: { children: React.ReactNode }) {
  const { me, loading } = useAuth()
  const location = useLocation()
  if (loading) return <Center h="100vh"><Loader /></Center>
  if (!me) return <Navigate to={withReferral('/login', referralCode(location.search))} replace />
  return <>{children}</>
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/authorize" element={<AuthorizePage />} />
        <Route path="/login" element={<AuthPage key="login" mode="login" />} />
        <Route path="/register" element={<AuthPage key="register" mode="register" />} />
        <Route path="/forgot" element={<ForgotPage />} />
        <Route element={<Protected><Layout /></Protected>}>
          <Route path="/" element={<HomePage />} />
          <Route path="/connect" element={<ConnectionPage />} />
          <Route path="/account" element={<AccountPage />} />
          <Route path="/plans" element={<PlansPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/servers" element={<ServersPage />} />
          <Route path="/invite" element={<InvitePage />} />
          <Route path="/tickets" element={<TicketsPage />} />
          <Route path="/help" element={<HelpPage />} />
          <Route path="/status" element={<ServiceStatusPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AuthProvider>
  )
}
