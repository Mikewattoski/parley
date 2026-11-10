import { describe, expect, it } from 'vitest';
import { parseServerEnvelope } from './protocol';

describe('parseServerEnvelope', () => {
  it('parses a saved message with its ids', () => {
    const raw = JSON.stringify({
      type: 'message', id: 7, cid: 'c1', room: 'general', from: 'alice', text: 'hi', ts: '2026-11-09T10:00:00Z',
    });
    expect(parseServerEnvelope(raw)).toEqual({
      type: 'message', id: 7, cid: 'c1', room: 'general', from: 'alice', text: 'hi', ts: '2026-11-09T10:00:00Z',
    });
  });

  it('defaults the member list of a joined frame to empty', () => {
    expect(parseServerEnvelope('{"type":"joined","room":"general"}')).toEqual({
      type: 'joined', room: 'general', users: [],
    });
  });

  it('parses an error that belongs to a message', () => {
    expect(parseServerEnvelope('{"type":"error","error":"could not save message","cid":"c9"}')).toEqual({
      type: 'error', error: 'could not save message', cid: 'c9',
    });
  });

  it.each([
    ['not json', 'not json'],
    ['unknown type', '{"type":"dance"}'],
    ['missing fields', '{"type":"message","room":"general"}'],
    ['wrong field types', '{"type":"presence","room":"general","from":"a","event":"waved"}'],
    ['not an object', '[1,2,3]'],
  ])('rejects %s', (_name, raw) => {
    expect(parseServerEnvelope(raw)).toBeNull();
  });

  it('rejects binary or non-string frames', () => {
    expect(parseServerEnvelope(new ArrayBuffer(8))).toBeNull();
  });
});
