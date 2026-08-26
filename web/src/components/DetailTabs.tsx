import { NavLink } from 'react-router-dom'

export function DetailTabs({
  tabs,
}: {
  tabs: { to: string; label: string; count?: number; end?: boolean }[]
}) {
  return (
    <nav className="detail-tabs" aria-label="Sections">
      {tabs.map((tab) => (
        <NavLink
          key={tab.to}
          to={tab.to}
          end={tab.end}
          className={({ isActive }) => (isActive ? 'active' : undefined)}
        >
          {tab.label}
          {tab.count !== undefined && <span className="count">({tab.count})</span>}
        </NavLink>
      ))}
    </nav>
  )
}
