import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import Layout from './routes/Layout'
import SignIn from './routes/SignIn'
import ProjectList from './routes/ProjectList'
import ProjectOverview from './routes/ProjectOverview'
import Backlog from './routes/Backlog'
import TicketBoard from './routes/TicketBoard'
import FeatureBoard from './routes/FeatureBoard'
import TicketDetail from './routes/TicketDetail'
import FeatureDetail from './routes/FeatureDetail'
import DecisionRegister from './routes/DecisionRegister'
import DecisionDetail from './routes/DecisionDetail'
import AdminAgents from './routes/AdminAgents'

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<SignIn />} />
        <Route element={<Layout />}>
          <Route path="/" element={<ProjectList />} />
          <Route path="/projects/:key" element={<ProjectOverview />} />
          <Route path="/projects/:key/backlog" element={<Backlog />} />
          <Route path="/projects/:key/board" element={<TicketBoard />} />
          <Route path="/projects/:key/features/board" element={<FeatureBoard />} />
          <Route path="/projects/:key/decisions" element={<DecisionRegister />} />
          <Route path="/tickets/:ref" element={<TicketDetail />} />
          <Route path="/features/:ref" element={<FeatureDetail />} />
          <Route path="/decisions/:ref" element={<DecisionDetail />} />
          <Route path="/admin/agents" element={<AdminAgents />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
