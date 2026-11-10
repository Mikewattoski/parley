/** Frames the client sends. */
export type ClientEnvelope =
  | { type: 'join'; room: string }
  | { type: 'leave'; room: string }
  | { type: 'message'; room: string; text: string; cid: string }
  | { type: 'typing'; room: string };

/** Frames the server sends. */
export type ServerEnvelope =
  | { type: 'welcome'; from: string }
  | { type: 'joined'; room: string; users: string[] }
  | { type: 'left'; room: string }
  | { type: 'presence'; room: string; from: string; event: 'joined' | 'left' }
  | { type: 'message'; id?: number; cid?: string; room: string; from: string; text: string; ts: string }
  | { type: 'typing'; room: string; from: string }
  | { type: 'error'; error: string; cid?: string };

function isString(value: unknown): value is string {
  return typeof value === 'string';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

/** Validates a raw WebSocket payload. Returns null for anything unexpected. */
export function parseServerEnvelope(data: unknown): ServerEnvelope | null {
  if (typeof data !== 'string') return null;

  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch {
    return null;
  }
  if (!isRecord(raw) || !isString(raw.type)) return null;

  switch (raw.type) {
    case 'welcome':
      return isString(raw.from) ? { type: 'welcome', from: raw.from } : null;
    case 'joined':
      if (!isString(raw.room)) return null;
      return {
        type: 'joined',
        room: raw.room,
        users: Array.isArray(raw.users) ? raw.users.filter(isString) : [],
      };
    case 'left':
      return isString(raw.room) ? { type: 'left', room: raw.room } : null;
    case 'presence':
      if (isString(raw.room) && isString(raw.from) && (raw.event === 'joined' || raw.event === 'left')) {
        return { type: 'presence', room: raw.room, from: raw.from, event: raw.event };
      }
      return null;
    case 'message':
      if (isString(raw.room) && isString(raw.from) && isString(raw.text) && isString(raw.ts)) {
        return {
          type: 'message',
          id: typeof raw.id === 'number' ? raw.id : undefined,
          cid: isString(raw.cid) ? raw.cid : undefined,
          room: raw.room,
          from: raw.from,
          text: raw.text,
          ts: raw.ts,
        };
      }
      return null;
    case 'typing':
      return isString(raw.room) && isString(raw.from)
        ? { type: 'typing', room: raw.room, from: raw.from }
        : null;
    case 'error':
      return isString(raw.error)
        ? { type: 'error', error: raw.error, cid: isString(raw.cid) ? raw.cid : undefined }
        : null;
    default:
      return null;
  }
}
