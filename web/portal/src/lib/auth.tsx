import { createContext, useContext, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, type Me } from './api'

interface Auth { me: Me | null; loading: boolean; refresh: () => Promise<void>; logout: () => Promise<void> }
const Ctx = createContext<Auth>({ me: null, loading: true, refresh: async () => {}, logout: async () => {} })

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      try { return await api.get<Me>('/api/portal/me') } catch (e) {
        if (e instanceof ApiError && e.status === 401) return null
        throw e
      }
    },
  })
  const logout = async () => { await api.post('/api/portal/logout'); qc.clear() }
  return <Ctx.Provider value={{ me: q.data ?? null, loading: q.isLoading, refresh: async () => { await qc.invalidateQueries({ queryKey: ['me'] }) }, logout }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)
