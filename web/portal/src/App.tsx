import { Navigate, Route, Routes } from 'react-router-dom'
import { Center, Loader } from '@mantine/core'
import { AuthProvider, useAuth } from './lib/auth'
import { Layout } from './components/Layout'
import AuthPage from './pages/AuthPage'
import ForgotPage from './pages/ForgotPage'
import HomePage from './pages/HomePage'
import PlansPage from './pages/PlansPage'
import OrdersPage from './pages/OrdersPage'
import ServersPage from './pages/ServersPage'
import InvitePage from './pages/InvitePage'
import TicketsPage from './pages/TicketsPage'
import HelpPage from './pages/HelpPage'

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
        <Route path="/login" element={<AuthPage mode="login" />} />
        <Route path="/register" element={<AuthPage mode="register" />} />
        <Route path="/forgot" element={<ForgotPage />} />
        <Route element={<Protected><Layout /></Protected>}>
          <Route path="/" element={<HomePage />} />
          <Route path="/plans" element={<PlansPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/servers" element={<ServersPage />} />
          <Route path="/invite" element={<InvitePage />} />
          <Route path="/tickets" element={<TicketsPage />} />
          <Route path="/help" element={<HelpPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AuthProvider>
  )
}
