import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthContext'
import Layout from './routes/Layout'
import SignIn from './routes/SignIn'
import Setup from './routes/Setup'
import Help from './routes/Help'
import ProjectList from './routes/ProjectList'
import ProjectOverview from './routes/ProjectOverview'
import Backlog from './routes/Backlog'
import NewTicket from './routes/NewTicket'
import TicketBoard from './routes/TicketBoard'
import FeatureBoard from './routes/FeatureBoard'
import TicketDetail, {
  TicketOverview,
  TicketLinksTab,
  TicketAttachmentsTab,
} from './routes/TicketDetail'
import FeatureDetail, {
  FeatureOverview,
  FeatureLinksTab,
  FeatureAttachmentsTab,
} from './routes/FeatureDetail'
import DecisionRegister from './routes/DecisionRegister'
import DecisionDetail, {
  DecisionOverview,
  DecisionLinksTab,
  DecisionAttachmentsTab,
} from './routes/DecisionDetail'
import ContentLibrary from './routes/ContentLibrary'
import ContentItemDetail, {
  ContentItemOverview,
  ContentItemLinksTab,
  ContentItemAttachmentsTab,
} from './routes/ContentItemDetail'
import ActivityFeed from './routes/ActivityFeed'
import Search from './routes/Search'
import Notifications from './routes/Notifications'
import AdminAgents from './routes/AdminAgents'
import AdminAccounts from './routes/AdminAccounts'
import AdminMaintenance from './routes/AdminMaintenance'

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<SignIn />} />
        <Route path="/setup" element={<Setup />} />
        <Route element={<Layout />}>
          <Route path="/" element={<ProjectList />} />
          <Route path="/help" element={<Help />} />
          <Route path="/projects/:key" element={<ProjectOverview />} />
          <Route path="/projects/:key/backlog" element={<Backlog />} />
          <Route path="/projects/:key/tickets/new" element={<NewTicket />} />
          <Route path="/projects/:key/board" element={<TicketBoard />} />
          <Route path="/projects/:key/features/board" element={<FeatureBoard />} />
          <Route path="/projects/:key/decisions" element={<DecisionRegister />} />
          <Route path="/projects/:key/plans" element={<ContentLibrary kind="plans" />} />
          <Route path="/projects/:key/documents" element={<ContentLibrary kind="documents" />} />
          <Route path="/projects/:key/activity" element={<ActivityFeed />} />
          <Route path="/search" element={<Search />} />
          <Route path="/notifications" element={<Notifications />} />
          <Route path="/tickets/:ref" element={<TicketDetail />}>
            <Route index element={<TicketOverview />} />
            <Route path="links" element={<TicketLinksTab />} />
            <Route path="attachments" element={<TicketAttachmentsTab />} />
          </Route>
          <Route path="/features/:ref" element={<FeatureDetail />}>
            <Route index element={<FeatureOverview />} />
            <Route path="links" element={<FeatureLinksTab />} />
            <Route path="attachments" element={<FeatureAttachmentsTab />} />
          </Route>
          <Route path="/decisions/:ref" element={<DecisionDetail />}>
            <Route index element={<DecisionOverview />} />
            <Route path="links" element={<DecisionLinksTab />} />
            <Route path="attachments" element={<DecisionAttachmentsTab />} />
          </Route>
          <Route path="/plans/:ref" element={<ContentItemDetail urlKind="plans" />}>
            <Route index element={<ContentItemOverview />} />
            <Route path="links" element={<ContentItemLinksTab />} />
            <Route path="attachments" element={<ContentItemAttachmentsTab />} />
          </Route>
          <Route path="/documents/:ref" element={<ContentItemDetail urlKind="documents" />}>
            <Route index element={<ContentItemOverview />} />
            <Route path="links" element={<ContentItemLinksTab />} />
            <Route path="attachments" element={<ContentItemAttachmentsTab />} />
          </Route>
          <Route path="/admin/agents" element={<AdminAgents />} />
          <Route path="/admin/accounts" element={<AdminAccounts />} />
          <Route path="/admin/maintenance" element={<AdminMaintenance />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </AuthProvider>
  )
}
