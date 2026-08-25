import { useEffect, useState } from 'react'
import { createAccount, listAccounts, changePassword, type AccountDetail } from '../api/accounts'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

function NewAccountForm({ onCreated }: { onCreated: (a: AccountDetail) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const created = await createAccount({ username, password, is_admin: isAdmin })
      onCreated(created)
      setUsername('')
      setPassword('')
      setIsAdmin(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <label>
        Username
        <input value={username} onChange={(e) => setUsername(e.target.value)} required />
      </label>
      <label>
        Password
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
      </label>
      <label>
        <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />{' '}
        Admin
      </label>
      {error && <p role="alert">{error}</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Creating…' : 'Create account'}
      </button>
    </form>
  )
}

/** A password-reset action a self-service form and an admin's reset
 * both use — the only difference is whether Old password is asked
 * for and sent (ADR-less split mirrors internal/service.ChangePassword's
 * own SelfService field). */
function ChangePasswordForm({
  username,
  selfService,
}: {
  username: string
  selfService: boolean
}) {
  const [oldPassword, setOldPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  async function submit() {
    setBusy(true)
    setError(null)
    setDone(false)
    try {
      await changePassword(username, newPassword, selfService ? oldPassword : undefined)
      setOldPassword('')
      setNewPassword('')
      setDone(true)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      {selfService && (
        <label>
          Old password
          <input
            type="password"
            value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)}
            required
          />
        </label>
      )}
      <label>
        New password
        <input
          type="password"
          value={newPassword}
          onChange={(e) => setNewPassword(e.target.value)}
          required
        />
      </label>
      {error && <p role="alert">{error}</p>}
      {done && <p>Password changed.</p>}
      <button type="submit" disabled={busy}>
        {busy ? 'Changing…' : 'Change password'}
      </button>
    </form>
  )
}

/** Human account administration (product spec §4.2/§13, Phase 7).
 * Every authenticated human sees "Change my password"; the account
 * list and creation form (admin-only server-side, via routeAdmin) are
 * additionally gated on the client by me.is_admin for the same
 * defense-in-depth reason AdminAgents does — a non-admin editor who
 * reaches this route by URL sees only their own section. */
export default function AdminAccounts() {
  const { me } = useAuth()
  const [accounts, setAccounts] = useState<AccountDetail[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [resetting, setResetting] = useState<string | null>(null)

  useEffect(() => {
    if (!me?.is_admin) return
    listAccounts()
      .then((page) => setAccounts(page.accounts))
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [me?.is_admin])

  const isHuman = me?.actor?.startsWith('human:') ?? false
  const myUsername = isHuman ? (me?.actor?.split(':')[1] ?? '') : ''

  return (
    <main>
      <h1>Accounts</h1>

      {isHuman ? (
        <>
          <h2>Change my password</h2>
          <ChangePasswordForm username={myUsername} selfService />
        </>
      ) : (
        <p>Password changes apply to human accounts; an agent identity has no password.</p>
      )}

      {me?.is_admin && (
        <>
          <h2>All accounts</h2>
          {error && <p role="alert">{error}</p>}
          {!accounts ? (
            <p>Loading accounts…</p>
          ) : (
            <ul>
              {accounts.map((a) => (
                <li key={a.username}>
                  <span>{a.username}</span> {a.is_admin && <span>(admin)</span>}{' '}
                  <button type="button" onClick={() => setResetting(resetting === a.username ? null : a.username)}>
                    Reset password
                  </button>
                  {resetting === a.username && (
                    <ChangePasswordForm username={a.username} selfService={false} />
                  )}
                </li>
              ))}
            </ul>
          )}
          <NewAccountForm onCreated={(a) => setAccounts([...(accounts ?? []), a])} />
        </>
      )}
    </main>
  )
}
