import { describe, expect, it } from 'vitest'
import { applyMarkdownAction } from './markdownActions'

describe('applyMarkdownAction', () => {
  it('wraps a selection in bold markers and keeps it selected', () => {
    const out = applyMarkdownAction({ value: 'hello world', start: 0, end: 5 }, 'bold')
    expect(out.value).toBe('**hello** world')
    expect(out.value.slice(out.start, out.end)).toBe('hello')
  })

  it('unwraps when the selection already carries the markers', () => {
    const out = applyMarkdownAction({ value: '**hello** world', start: 0, end: 9 }, 'bold')
    expect(out.value).toBe('hello world')
    expect(out.value.slice(out.start, out.end)).toBe('hello')
  })

  it('unwraps when the markers sit just outside the selection', () => {
    const out = applyMarkdownAction({ value: '**hello** world', start: 2, end: 7 }, 'bold')
    expect(out.value).toBe('hello world')
    expect(out.value.slice(out.start, out.end)).toBe('hello')
  })

  it('wraps an empty selection so typing lands between the markers', () => {
    const out = applyMarkdownAction({ value: 'ab', start: 1, end: 1 }, 'italic')
    expect(out.value).toBe('a**b')
    expect(out.start).toBe(2)
    expect(out.end).toBe(2)
  })

  it('prefixes every line the selection touches', () => {
    const out = applyMarkdownAction({ value: 'one\ntwo\nthree', start: 0, end: 7 }, 'bullet')
    expect(out.value).toBe('- one\n- two\nthree')
  })

  it('removes the prefix when every touched line already has it', () => {
    const out = applyMarkdownAction({ value: '- one\n- two', start: 0, end: 11 }, 'bullet')
    expect(out.value).toBe('one\ntwo')
  })

  it('prefixes the line the caret sits on when nothing is selected', () => {
    const out = applyMarkdownAction({ value: 'one\ntwo', start: 5, end: 5 }, 'heading')
    expect(out.value).toBe('one\n## two')
  })

  it('inserts a link and selects the url placeholder', () => {
    const out = applyMarkdownAction({ value: 'see docs', start: 4, end: 8 }, 'link')
    expect(out.value).toBe('see [docs](url)')
    expect(out.value.slice(out.start, out.end)).toBe('url')
  })

  it('inserts a link with placeholder text when nothing is selected', () => {
    const out = applyMarkdownAction({ value: '', start: 0, end: 0 }, 'link')
    expect(out.value).toBe('[text](url)')
    expect(out.value.slice(out.start, out.end)).toBe('url')
  })
})
