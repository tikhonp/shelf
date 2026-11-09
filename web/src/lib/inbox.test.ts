import { expect, test } from 'vitest'
import type { Op, ProposalEvent } from '../api/agents'
import { eventLine, opLabel, opName } from './inbox'

const tag = (id: string, field?: string, tag_key?: string): Op => ({ id, type: 'tag', field, tag_key, tracks: [] })

test('operation labels use field names and preserve raw keys', () => {
  expect(opLabel(tag('1', 'releasedate'))).toBe('Release date')
  expect(opLabel(tag('2', 'albumartists'))).toBe('Album artists')
  expect(opLabel(tag('3', 'unknown_field'))).toBe('Unknown field')
  expect(opLabel(tag('4', undefined, 'ALBUMARTISTS'))).toBe('ALBUMARTISTS')
  expect(opLabel({ id: '5', type: 'cover', action: 'replace' })).toBe('Replace cover')
  expect(opLabel({ id: '6', type: 'cover', action: 'remove' })).toBe('Remove cover')
  expect(opName([], 'missing_field')).toBe('Missing field')
})

test('discussion lines name operations with or without their records', () => {
  const e: ProposalEvent = { id: 1, kind: 'proposal_created', actor_kind: 'agent', actor: 'Agent', at: '', data: { ops: ['a', 'releasedate'] } }
  expect(eventLine(e, [tag('a', 'albumartists')])).toBe('Agent proposed Album artists, Release date')
  expect(eventLine(e)).toBe('Agent proposed A, Release date')
})
