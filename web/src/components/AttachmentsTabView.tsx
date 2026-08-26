import { AttachmentList } from './AttachmentList'
import type { Attachment } from '../api/types'

export function AttachmentsTabView({
  ownerRef,
  attachments,
  onChange,
  canEdit,
}: {
  ownerRef: string
  attachments: Attachment[]
  onChange: (attachments: Attachment[]) => void
  canEdit: boolean
}) {
  return (
    <section className="detail-section">
      <AttachmentList ownerRef={ownerRef} attachments={attachments} onChange={onChange} canEdit={canEdit} />
    </section>
  )
}
