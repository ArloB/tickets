import type { APIRequestContext, Page } from '@playwright/test'
import { adminPassword } from '../playwright.config.js'

/** ValidProjectKey (internal/domain/reference.go): uppercase letters
 * and digits, 2-10 chars, starting with a letter. Tests share one
 * server/database for the whole run, so every project needs its own
 * key — this generates one that's astronomically unlikely to collide
 * within a single run. */
export function randomKey(prefix = 'E2E'): string {
  const suffix = Math.floor(Math.random() * 900 + 100)
  return `${prefix}${suffix}`
}

export async function login(page: Page, username = 'admin', password = adminPassword): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill(username)
  await page.getByLabel('Password').fill(password)
  await page.click('button[type="submit"]')
  await page.waitForURL('**/')
}

/** Reads the CSRF token for whichever session `ctx` already carries
 * (browser cookies for `page.request`, or a request-context's own
 * login for an out-of-band actor) — GET /auth/me is itself
 * routeViewer, so this never needs a token to call. */
async function csrfToken(ctx: APIRequestContext): Promise<string> {
  const resp = await ctx.get('/api/v1/auth/me')
  const body = (await resp.json()) as { csrf_token?: string }
  if (!body.csrf_token) throw new Error('no csrf_token on /auth/me — is this session authenticated?')
  return body.csrf_token
}

/** Logs a bare APIRequestContext (the `request` fixture, or a fresh
 * `request.newContext()`) into its own session, independent of any
 * browser page — used for an out-of-band concurrent actor in the
 * conflict-resolution spec, where the point is that this session's
 * writes are invisible to a browser session mid-edit until it
 * refetches. */
export async function apiLogin(
  ctx: APIRequestContext,
  username = 'admin',
  password = adminPassword,
): Promise<void> {
  const resp = await ctx.post('/api/v1/auth/login', { data: { username, password } })
  if (!resp.ok()) {
    throw new Error(`login failed -> ${resp.status()}: ${await resp.text()}`)
  }
}

export async function apiPost<T>(ctx: APIRequestContext, path: string, data: unknown): Promise<T> {
  return apiRequest<T>(ctx, 'POST', path, data)
}

/** `If-Match: "<version>"` per docs/contracts/concurrency.md — a
 * double-quoted decimal integer, not a bare number. */
export function ifMatchHeader(version: number): Record<string, string> {
  return { 'If-Match': `"${version}"` }
}

export async function apiRequest<T>(
  ctx: APIRequestContext,
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  data?: unknown,
  extraHeaders: Record<string, string> = {},
): Promise<T> {
  const csrf = await csrfToken(ctx)
  const resp = await ctx.fetch(`/api/v1${path}`, {
    method,
    headers: { 'X-CSRF-Token': csrf, ...extraHeaders },
    data,
  })
  if (!resp.ok()) {
    throw new Error(`${method} ${path} -> ${resp.status()}: ${await resp.text()}`)
  }
  return resp.json() as Promise<T>
}

export interface CreatedProject {
  key: string
}

export async function createProject(ctx: APIRequestContext, key: string, title: string): Promise<CreatedProject> {
  return apiPost<CreatedProject>(ctx, '/projects', { key, title, description: 'created by the e2e suite' })
}

export interface CreatedTicket {
  ref: string
  version: number
}

export async function createTicket(
  ctx: APIRequestContext,
  projectKey: string,
  overrides: Partial<{
    type: string
    title: string
    description: string
    priority: string
    severity: string | null
  }> = {},
): Promise<CreatedTicket> {
  return apiPost<CreatedTicket>(ctx, `/projects/${projectKey}/tickets`, {
    type: 'task',
    title: 'e2e seed ticket',
    description: 'seeded by the e2e suite',
    priority: 'medium',
    ...overrides,
  })
}
