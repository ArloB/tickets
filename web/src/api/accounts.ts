import { apiFetch } from './client'

/** Mirrors internal/httpapi/accounts.go's accountDetail — human
 * account management (product spec §4.2/§13, Phase 7). Admin-only,
 * except changePassword's self-service path. */
export interface AccountDetail {
  username: string
  is_admin: boolean
  created_at?: string
}

export interface AccountsPage {
  accounts: AccountDetail[]
}

export async function listAccounts(): Promise<AccountsPage> {
  return apiFetch<AccountsPage>('/accounts')
}

export interface CreateAccountInput {
  username: string
  password: string
  is_admin?: boolean
}

export async function createAccount(input: CreateAccountInput): Promise<AccountDetail> {
  return apiFetch<AccountDetail>('/accounts', { method: 'POST', body: input })
}

/** POST /accounts/{username}/password. oldPassword is required and
 * verified for a human changing their own password; an admin
 * resetting a different account's password omits it. */
export async function changePassword(
  username: string,
  newPassword: string,
  oldPassword?: string,
): Promise<void> {
  await apiFetch<void>(`/accounts/${encodeURIComponent(username)}/password`, {
    method: 'POST',
    body: { old_password: oldPassword, new_password: newPassword },
  })
}
