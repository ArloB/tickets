const fundamentals: Array<{ title: string; body: string }> = [
  {
    title: 'Projects',
    body: 'Everything lives inside a project — a short key like ABC groups its own tickets, features, decisions, plans, and documents.',
  },
  {
    title: 'Tickets and features',
    body: 'A feature is a body of work; tickets are the tasks, bugs, security issues, and chores that make it up. Every ticket belongs to exactly one feature.',
  },
  {
    title: 'Decisions, plans, and documents',
    body: 'Decisions record a choice and why it was made, with full edit history. Plans and documents hold longer-form Markdown, uploaded files, or links to either.',
  },
  {
    title: 'Comments and links',
    body: 'Any record can carry comments, and can be linked to or associated with any other — across tickets, features, decisions, plans, and documents alike.',
  },
  {
    title: 'Search',
    body: 'One search box covers everything above at once — titles, bodies, comments, attachments, and links.',
  },
]

export function FundamentalsList() {
  return (
    <ol>
      {fundamentals.map((f) => (
        <li key={f.title}>
          <strong>{f.title}</strong>
          <p>{f.body}</p>
        </li>
      ))}
    </ol>
  )
}
