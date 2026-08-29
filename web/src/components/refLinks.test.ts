import { describe, expect, it } from 'vitest'
import { scanRefs } from './refLinks'

// scanRefs is the JS half of docs/contracts/references.md's
// "Recognition in text" grammar; internal/domain/scan.go is the Go
// half. These cases are the ones that would silently diverge — the
// kind-code alternation, the leading-zero rule, and the identifier
// boundaries — asserted here so a change to one side fails visibly
// rather than producing a link the server would never call a mention.
describe('scanRefs', () => {
  it('finds every kind, bare and #-prefixed', () => {
    const got = scanRefs('ABC-1 #ABC-2 ABC-F3 ABC-D4 ABC-P5 ABC-DOC6', '')
    expect(got.map((m) => m.token)).toEqual([
      'ABC-1',
      'ABC-2',
      'ABC-F3',
      'ABC-D4',
      'ABC-P5',
      'ABC-DOC6',
    ])
  })

  it('keeps the matched text distinct from the canonical token', () => {
    const [match] = scanRefs('#ABC-2', '')
    expect(match.text).toBe('#ABC-2')
    expect(match.token).toBe('ABC-2')
  })

  it('matches DOC before the D branch', () => {
    expect(scanRefs('ABC-DOC9', '').map((m) => m.token)).toEqual(['ABC-DOC9'])
  })

  it('resolves the short form only against a project scope', () => {
    expect(scanRefs('Fixes #123.', 'ABC').map((m) => m.token)).toEqual(['ABC-123'])
    expect(scanRefs('Fixes #123.', '')).toEqual([])
  })

  it('rejects a sequence with a leading zero', () => {
    expect(scanRefs('ABC-01', '')).toEqual([])
  })

  it('rejects a project key that is too short, lowercase, or digit-led', () => {
    expect(scanRefs('A-1 abc-1 1BC-1', '')).toEqual([])
  })

  it('rejects an unknown kind letter', () => {
    expect(scanRefs('ABC-X1', '')).toEqual([])
  })

  // Matching scan.go's isBoundaryOK exactly, including its one
  // surprise: "XABC-1" is not the tail of a longer identifier, it is
  // a reference into project XABC, since XABC is itself a valid key.
  it('rejects a match bounded by a word character', () => {
    expect(scanRefs('_ABC-1', '')).toEqual([])
    expect(scanRefs('ABC-1x', '')).toEqual([])
    expect(scanRefs('XABC-1', '').map((m) => m.token)).toEqual(['XABC-1'])
  })
})
