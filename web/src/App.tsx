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
import ContentLibrary from './routes/ContentLibrary'
import ContentItemDetail from './routes/ContentItemDetail'
import ActivityFeed from './routes/ActivityFeed'
import Search from './routes/Search'
import Notifications from './routes/Notifications'
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
          <Route path="/projects/:key/plans" element={<ContentLibrary kind="plans" />} />
          <Route path="/projects/:key/documents" element={<ContentLibrary kind="documents" />} />
          <Route path="/projects/:key/activity" element={<ActivityFeed />} />
          <Route path="/search" element={<Search />} />
          <Route path="/notifications" element={<Notifications />} />
          <Route path="/tickets/:ref" element={<TicketDetail />} />
          <Route path="/features/:ref" element={<FeatureDetail />} />
          <Route path="/decisions/:ref" element={<DecisionDetail />} />
          <Route path="/plans/:ref" element={<ContentItemDetail urlKind="plans" />} />
          <Route path="/documents/:ref" element={<ContentItemDetail urlKind="documents" />} />
          <Route path="/admin/agents" element={<AdminAgents />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
