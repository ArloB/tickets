import { Link } from 'react-router-dom'
import { detailRoute } from '../api/refs'
import { RelationshipsSection } from './RelationshipsSection'
import { AssociationsSection } from './AssociationsSection'
import { LinksSection } from './LinksSection'
import type { Backlink, ExternalLink, RelationshipType } from '../api/types'

export function LinksTabView({
  entityRef,
  relationships,
  onRelationshipsChange,
  associated,
  onAssociatedChange,
  links,
  onLinksChange,
  backlinks,
  canEdit,
}: {
  entityRef: string
  relationships?: { type: RelationshipType; other: string }[]
  onRelationshipsChange?: (relationships: { type: RelationshipType; other: string }[]) => void
  associated: string[]
  onAssociatedChange: (associated: string[]) => void
  links: ExternalLink[]
  onLinksChange: (links: ExternalLink[]) => void
  backlinks: Backlink[]
  canEdit: boolean
}) {
  return (
    <>
      {relationships !== undefined && onRelationshipsChange && (
        <section className="detail-section">
          <h2>Relationships</h2>
          <RelationshipsSection
            ticketRef={entityRef}
            relationships={relationships}
            onChange={onRelationshipsChange}
            canEdit={canEdit}
          />
        </section>
      )}

      <section className="detail-section">
        <h2>Associations</h2>
        <AssociationsSection
          entityRef={entityRef}
          associated={associated}
          onChange={onAssociatedChange}
          canEdit={canEdit}
        />
      </section>

      <section className="detail-section">
        <h2>External links</h2>
        <LinksSection entityRef={entityRef} links={links} onChange={onLinksChange} canEdit={canEdit} />
      </section>

      <section className="detail-section">
        <h2>Backlinks</h2>
        {backlinks.length === 0 ? (
          <p>None.</p>
        ) : (
          <ul>
            {backlinks.map((b) => (
              <li key={`${b.ref}-${b.comment_id ?? 'body'}`}>
                <Link to={detailRoute(b.ref)}>{b.ref}</Link>
                {b.comment_id !== undefined ? ` (comment #${b.comment_id})` : ' (description)'}
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  )
}
