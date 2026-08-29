import { useCallback, useEffect, useState } from 'react'
import { Link, Outlet, useOutletContext, useParams } from 'react-router-dom'
import { getFeature } from '../api/features'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listBacklinks } from '../api/backlinks'
import { listAttachments } from '../api/attachments'
import { listComments } from '../api/comments'
import { ApiError } from '../api/client'
import { useEntityChanged } from '../api/events'
import { Markdown } from '../components/Markdown'
import { FeatureFieldsForm } from '../components/FeatureFieldsForm'
import { CommentsSection } from '../components/CommentsSection'
import { DetailTabs } from '../components/DetailTabs'
import { LinksTabView } from '../components/LinksTabView'
import { AttachmentsTabView } from '../components/AttachmentsTabView'
import { SubscribeButton } from '../components/SubscribeButton'
import { StatusChip } from '../components/StatusChip'
import { useAuth } from '../auth/AuthContext'
import type {
  Attachment,
  Backlink,
  CommentDetail,
  ExternalLink,
  FeatureDetail as FeatureDetailDto,
} from '../api/types'

interface FeatureContext {
  feature: FeatureDetailDto
  comments: CommentDetail[]
  setComments: (comments: CommentDetail[]) => void
  links: ExternalLink[]
  setLinks: (links: ExternalLink[]) => void
  associated: string[]
  setAssociated: (associated: string[]) => void
  backlinks: Backlink[]
  attachments: Attachment[]
  setAttachments: (attachments: Attachment[]) => void
  canEdit: boolean
}

function useFeatureContext() {
  return useOutletContext<FeatureContext>()
}

export default function FeatureDetail() {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [feature, setFeature] = useState<FeatureDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [associated, setAssociated] = useState<string[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [attachments, setAttachments] = useState<Attachment[] | null>(null)
  const [comments, setComments] = useState<CommentDetail[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)

  const load = useCallback((clear: boolean) => {
    if (clear) {
      setFeature(null)
      setLinks(null)
      setAssociated(null)
      setBacklinks(null)
      setAttachments(null)
      setComments(null)
      setError(null)
      setEditing(false)
    }
    Promise.all([
      getFeature(ref),
      listLinks(ref),
      listAssociations(ref),
      listBacklinks(ref),
      listAttachments(ref),
      listComments(ref),
    ])
      .then(([f, l, a, b, at, c]) => {
        setFeature(f)
        setLinks(l.links)
        setAssociated(a.associated)
        setBacklinks(b.backlinks)
        setAttachments(at.attachments)
        setComments(c.comments)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [ref])

  useEffect(() => {
    load(true)
  }, [load])

  // Suppressed while editing — see TicketDetail's identical guard doc.
  useEntityChanged(feature?.ref, useCallback(() => {
    if (!editing) load(false)
  }, [editing, load]))

  if (error) return <p role="alert">{error}</p>
  if (!feature || !links || !associated || !backlinks || !attachments || !comments) {
    return <p>Loading feature…</p>
  }

  const canEdit = me?.permission === 'editor'

  if (editing) {
    return (
      <main>
        <h1>
          Edit {feature.title} <span>({feature.ref})</span>
        </h1>
        <FeatureFieldsForm
          feature={feature}
          onSaved={(updated) => {
            setFeature({ ...feature, ...updated })
            setEditing(false)
          }}
          onCancel={() => setEditing(false)}
        />
      </main>
    )
  }

  const linksCount = associated.length + links.length + backlinks.length

  return (
    <main>
      <h1>
        {feature.title} <span>({feature.ref})</span>
      </h1>
      <p>
        Project: <Link to={`/projects/${feature.project}`}>{feature.project}</Link>
      </p>
      <p>
        <StatusChip value={feature.status} kind="status" /> ·{' '}
        <StatusChip value={feature.priority} kind="priority" />
      </p>
      {canEdit && <button onClick={() => setEditing(true)}>Edit</button>}
      <SubscribeButton targetRef={feature.ref} canEdit={canEdit} />

      <DetailTabs
        tabs={[
          { to: '.', label: 'Overview', end: true },
          { to: 'links', label: 'Links', count: linksCount },
          { to: 'attachments', label: 'Attachments', count: attachments.length },
        ]}
      />

      <Outlet
        context={
          {
            feature,
            comments,
            setComments,
            links,
            setLinks,
            associated,
            setAssociated,
            backlinks,
            attachments,
            setAttachments,
            canEdit,
          } satisfies FeatureContext
        }
      />
    </main>
  )
}

export function FeatureOverview() {
  const { feature, comments, setComments, canEdit } = useFeatureContext()
  return (
    <>
      <section className="detail-section">
        <h2>Description</h2>
        <Markdown projectKey={feature.project}>{feature.description}</Markdown>
      </section>

      <section className="detail-section">
        <h2>Comments</h2>
        <CommentsSection
          entityRef={feature.ref}
          comments={comments}
          onChange={setComments}
          canEdit={canEdit}
        />
      </section>
    </>
  )
}

export function FeatureLinksTab() {
  const { feature, associated, setAssociated, links, setLinks, backlinks, canEdit } = useFeatureContext()
  return (
    <LinksTabView
      entityRef={feature.ref}
      associated={associated}
      onAssociatedChange={setAssociated}
      links={links}
      onLinksChange={setLinks}
      backlinks={backlinks}
      canEdit={canEdit}
    />
  )
}

export function FeatureAttachmentsTab() {
  const { feature, attachments, setAttachments, canEdit } = useFeatureContext()
  return (
    <AttachmentsTabView
      ownerRef={feature.ref}
      attachments={attachments}
      onChange={setAttachments}
      canEdit={canEdit}
    />
  )
}
