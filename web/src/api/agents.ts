import { apiFetch } from './client'

/** Mirrors internal/httpapi/admin.go's agentDetail — admin-only agent
 * management (product spec §4.1). */
export interface AgentDetail {
  name: string
  description: string
  owner?: string
  created_at: string
}

export interface AgentsPage {
  agents: AgentDetail[]
}

export async function listAgents(): Promise<AgentsPage> {
  return apiFetch<AgentsPage>('/agents')
}

export interface CreateAgentInput {
  name: string
  description: string
}

export async function createAgent(input: CreateAgentInput): Promise<AgentDetail> {
  return apiFetch<AgentDetail>('/agents', { method: 'POST', body: input })
}

/** The raw token value is present only in this response shape — ADR
 * 0004: shown once, at creation, never retrievable again. Every later
 * read of the same token returns AgentTokenSummary instead, which has
 * no `token` field at all. */
export interface AgentTokenCreated {
  id: number
  token: string
  description: string
  expires_at?: string
}

export interface AgentTokenSummary {
  id: number
  description: string
  created_at: string
  expires_at?: string
  revoked_at?: string
}

export interface AgentTokensPage {
  tokens: AgentTokenSummary[]
}

export async function listAgentTokens(name: string): Promise<AgentTokensPage> {
  return apiFetch<AgentTokensPage>(`/agents/${encodeURIComponent(name)}/tokens`)
}

export interface CreateAgentTokenInput {
  description: string
  expiresAt?: string
}

export async function createAgentToken(
  name: string,
  input: CreateAgentTokenInput,
): Promise<AgentTokenCreated> {
  return apiFetch<AgentTokenCreated>(`/agents/${encodeURIComponent(name)}/tokens`, {
    method: 'POST',
    body: { description: input.description, expires_at: input.expiresAt },
  })
}

/** DELETE /agents/{name}/tokens/{id} takes no If-Match — token
 * revocation isn't a versioned entity mutation (docs/contracts/
 * concurrency.md's exceptions list), just a one-way state flip. */
export async function revokeAgentToken(name: string, id: number): Promise<void> {
  await apiFetch<void>(`/agents/${encodeURIComponent(name)}/tokens/${id}`, { method: 'DELETE' })
}
