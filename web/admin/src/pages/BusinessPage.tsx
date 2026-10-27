import { Navigate, useLocation, useParams } from 'react-router-dom'

export default function BusinessPage() {
  const { section } = useParams()
  const { search, hash } = useLocation()
  return <Navigate replace to={{ pathname: section === 'content' ? '/articles' : '/coupons', search, hash }} />
}
