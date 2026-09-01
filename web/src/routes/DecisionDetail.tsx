import { useCallback, useEffect, useState } from 'react'
import { Link, Outlet, useOutletContext, useParams } from 'react-router-dom'
import { getDecision, getDecisionDiff, listDecisionVersions } from '../api/decisions'
import { listLinks } from '../api/links'
import { listAssociations } from '../api/associations'
import { listBacklinks } from '../api/backlinks'
import { listAttachments } from '../api/attachments'
import { listComments } from '../api/comments'
import { detailRoute } from '../api/refs'
import { ApiError } from '../api/client'
import { useEntityChanged } from '../api/events'
import { Markdown } from '../components/Markdown'
import { DecisionFieldsForm } from '../components/DecisionFieldsForm'
import { CommentsSection } from '../components/CommentsSection'
import { DetailTabs } from '../components/DetailTabs'
import { LinksTabView } from '../components/LinksTabView'
import { AttachmentsTabView } from '../components/AttachmentsTabView'
import { DiffView } from '../components/DiffView'
import { SubscribeButton } from '../components/SubscribeButton'
import { StatusChip } from '../components/StatusChip'
import { useAuth } from '../auth/AuthContext'
import type {
  Attachment,
  Backlink,
  CommentDetail,
  DecisionDiff,
  DecisionDetail as DecisionDetailDto,
  DecisionVersion,
  ExternalLink,
} from '../api/types'

interface DecisionContext {
  decision: DecisionDetailDto
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

function useDecisionContext() {
  return useOutletContext<DecisionContext>()
}

function VersionHistory({ decision }: { decision: DecisionDetailDto }) {
  const [versions, setVersions] = useState<DecisionVersion[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [from, setFrom] = useState<number | null>(null)
  const [to, setTo] = useState<number>(decision.version)
  const [diff, setDiff] = useState<DecisionDiff | null>(null)
  const [diffError, setDiffError] = useState<string | null>(null)

  useEffect(() => {
    setVersions(null)
    setDiff(null)
    listDecisionVersions(decision.ref)
      .then((page) => {
        setVersions(page.versions)
        setFrom(page.versions.length > 0 ? page.versions[0].version : null)
      })
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [decision.ref])

  async function showDiff() {
    if (from === null) return
    setDiffError(null)
    try {
      setDiff(await getDecisionDiff(decision.ref, from, to))
    } catch (err) {
      setDiffError(err instanceof ApiError ? err.message : String(err))
    }
  }

  if (error) return <p role="alert">{error}</p>
  if (!versions) return <p>Loading version history…</p>
  if (versions.length === 0) return <p>No prior versions — this is the only version.</p>

  const allVersions = [...versions.map((v) => v.version), decision.version]

  return (
    <div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Version</th>
              <th>Title</th>
              <th>Status</th>
              <th>Edited by</th>
              <th>Edited at</th>
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.version}>
                <td>{v.version}</td>
                <td>{v.title}</td>
                <td>
                  <StatusChip value={v.status} kind="decision" />
                </td>
                <td>{v.edited_by}</td>
                <td>{new Date(v.created_at).toLocaleString()}</td>
              </tr>
            ))}
            <tr>
              <td>{decision.version} (current)</td>
              <td>{decision.title}</td>
              <td>
                <StatusChip value={decision.status} kind="decision" />
              </td>
              <td colSpan={2}></td>
            </tr>
          </tbody>
        </table>
      </div>

      <form
        className="inline-form"
        onSubmit={(e) => {
          e.preventDefault()
          if (diff) {
            setDiff(null)
            setDiffError(null)
          } else {
            void showDiff()
          }
        }}
      >
        <label>
          From
          <select
            value={from ?? ''}
            onChange={(e) => {
              setFrom(Number(e.target.value))
              setDiff(null)
            }}
          >
            {allVersions.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label>
          To
          <select
            value={to}
            onChange={(e) => {
              setTo(Number(e.target.value))
              setDiff(null)
            }}
          >
            {allVersions.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <button type="submit">{diff ? 'Dismiss diff' : 'Show diff'}</button>
      </form>

      {diffError && <p role="alert">{diffError}</p>}
      {diff && (
        <div data-testid="version-diff">
          {diff.status_from !== diff.status_to && (
            <p>
              Status: {diff.status_from} → {diff.status_to}
            </p>
          )}
          {(
            [
              ['Title', diff.title],
              ['Context', diff.context],
              ['Decision', diff.decision],
              ['Rationale', diff.rationale],
              ['Consequences', diff.consequences],
            ] as const
          ).map(([label, lines]) => (
            <div key={label}>
              <h3>{label}</h3>
              <DiffView lines={lines} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default function DecisionDetail() {
  const { ref = '' } = useParams()
  const { me } = useAuth()
  const [decision, setDecision] = useState<DecisionDetailDto | null>(null)
  const [links, setLinks] = useState<ExternalLink[] | null>(null)
  const [associated, setAssociated] = useState<string[] | null>(null)
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null)
  const [attachments, setAttachments] = useState<Attachment[] | null>(null)
  const [comments, setComments] = useState<CommentDetail[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)

  const load = useCallback((clear: boolean) => {
    if (clear) {
      setDecision(null)
      setLinks(null)
      setAssociated(null)
      setBacklinks(null)
      setAttachments(null)
      setComments(null)
      setError(null)
      setEditing(false)
    }
    Promise.all([
      getDecision(ref),
      listLinks(ref),
      listAssociations(ref),
      listBacklinks(ref),
      listAttachments(ref),
      listComments(ref),
    ])
      .then(([d, l, a, b, at, c]) => {
        setDecision(d)
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
  useEntityChanged(decision?.ref, useCallback(() => {
    if (!editing) load(false)
  }, [editing, load]))

  if (error) return <p role="alert">{error}</p>
  if (!decision || !links || !associated || !backlinks || !attachments || !comments) {
    return <p>Loading decision…</p>
  }

  const canEdit = me?.permission === 'editor'

  if (editing) {
    return (
      <main>
        <h1>
          Edit {decision.title} <span>({decision.ref})</span>
        </h1>
        <DecisionFieldsForm
          decision={decision}
          onSaved={(updated) => {
            setDecision({ ...decision, ...updated })
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
        {decision.title} <span>({decision.ref})</span>
      </h1>
      <p>
        Project: <Link to={`/projects/${decision.project}`}>{decision.project}</Link> ·{' '}
        <StatusChip value={decision.status} kind="decision" />
      </p>
      {decision.superseded_by && (
        <p>
          Superseded by:{' '}
          <Link to={detailRoute(decision.superseded_by)}>{decision.superseded_by}</Link>
        </p>
      )}
      {canEdit && <button onClick={() => setEditing(true)}>Edit</button>}
      <SubscribeButton targetRef={decision.ref} canEdit={canEdit} />

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
            decision,
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
          } satisfies DecisionContext
        }
      />
    </main>
  )
}

export function DecisionOverview() {
  const { decision, comments, setComments, canEdit } = useDecisionContext()
  return (
    <>
      <section className="detail-section">
        <h2>Context</h2>
        <Markdown projectKey={decision.project}>{decision.context}</Markdown>
      </section>

      <section className="detail-section">
        <h2>Decision</h2>
        <Markdown projectKey={decision.project}>{decision.decision}</Markdown>
      </section>

      <section className="detail-section">
        <h2>Rationale</h2>
        <Markdown projectKey={decision.project}>{decision.rationale}</Markdown>
      </section>

      <section className="detail-section">
        <h2>Consequences</h2>
        <Markdown projectKey={decision.project}>{decision.consequences}</Markdown>
      </section>

      <section className="detail-section">
        <h2>Version history</h2>
        <VersionHistory decision={decision} />
      </section>

      <section className="detail-section">
        <h2>Comments</h2>
        <CommentsSection
          entityRef={decision.ref}
          comments={comments}
          onChange={setComments}
          canEdit={canEdit}
        />
      </section>
    </>
  )
}

export function DecisionLinksTab() {
  const { decision, associated, setAssociated, links, setLinks, backlinks, canEdit } = useDecisionContext()
  return (
    <LinksTabView
      entityRef={decision.ref}
      associated={associated}
      onAssociatedChange={setAssociated}
      links={links}
      onLinksChange={setLinks}
      backlinks={backlinks}
      canEdit={canEdit}
    />
  )
}

export function DecisionAttachmentsTab() {
  const { decision, attachments, setAttachments, canEdit } = useDecisionContext()
  return (
    <AttachmentsTabView
      ownerRef={decision.ref}
      attachments={attachments}
      onChange={setAttachments}
      canEdit={canEdit}
    />
  )
}
