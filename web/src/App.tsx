import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import Layout from './routes/Layout'
import SignIn from './routes/SignIn'
import ProjectList from './routes/ProjectList'
import ProjectOverview from './routes/ProjectOverview'
import Backlog from './routes/Backlog'
import TicketDetail from './routes/TicketDetail'
import FeatureDetail from './routes/FeatureDetail'
import DecisionRegister from './routes/DecisionRegister'
import DecisionDetail from './routes/DecisionDetail'

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<SignIn />} />
        <Route element={<Layout />}>
          <Route path="/" element={<ProjectList />} />
          <Route path="/projects/:key" element={<ProjectOverview />} />
          <Route path="/projects/:key/backlog" element={<Backlog />} />
          <Route path="/projects/:key/decisions" element={<DecisionRegister />} />
          <Route path="/tickets/:ref" element={<TicketDetail />} />
          <Route path="/features/:ref" element={<FeatureDetail />} />
          <Route path="/decisions/:ref" element={<DecisionDetail />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
