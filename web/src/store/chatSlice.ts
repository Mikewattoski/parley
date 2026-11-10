import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import type { ApiMessage, Message } from '../types';
import { logout, sessionExpired } from './authSlice';

export interface RoomChat {
  messages: Message[];
  members: string[]; // a multiset: one entry per connection, so two tabs count twice
  typing: Record<string, number>; // username -> when the last typing event arrived
  hasMore: boolean;
  nextCursor: number | null;
  loadingHistory: boolean;
  historyError: string | null;
  loaded: boolean; // the first page of history has been fetched
}

export interface ChatState {
  rooms: Record<string, RoomChat>;
}

export interface ReceivedMessage {
  id?: number;
  cid?: string;
  room: string;
  from: string;
  text: string;
  ts: string;
}

function emptyRoom(): RoomChat {
  return {
    messages: [],
    members: [],
    typing: {},
    hasMore: false,
    nextCursor: null,
    loadingHistory: false,
    historyError: null,
    loaded: false,
  };
}

/** Returned by selectors for rooms nobody has touched yet. Never mutate it. */
export const EMPTY_ROOM: RoomChat = emptyRoom();

const initialState: ChatState = { rooms: {} };

function roomState(state: ChatState, name: string): RoomChat {
  state.rooms[name] ??= emptyRoom();
  return state.rooms[name];
}

/** Confirmed messages ordered by id, then unconfirmed ones in the order they were sent. */
function normalize(messages: Message[]): Message[] {
  const confirmed = messages
    .filter((m) => m.id !== undefined)
    .sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
  const unconfirmed = messages.filter((m) => m.id === undefined);
  return [...confirmed, ...unconfirmed];
}

function fromApi(m: ApiMessage): Message {
  return { key: `m${m.id}`, id: m.id, room: m.room, from: m.from, text: m.text, ts: m.ts, status: 'sent' };
}

/**
 * Merges a page of server history into the list: known ids are skipped, and a
 * local message that never got its acknowledgement is dropped when the server
 * already has it (same author, same text), e.g. after a reconnect.
 */
function mergeHistory(existing: Message[], incoming: Message[]): Message[] {
  const known = new Set(existing.filter((m) => m.id !== undefined).map((m) => m.id));
  const fresh = incoming.filter((m) => !known.has(m.id));
  const kept = existing.filter(
    (local) => local.id !== undefined || !fresh.some((f) => f.from === local.from && f.text === local.text),
  );
  return normalize([...kept, ...fresh]);
}

const chatSlice = createSlice({
  name: 'chat',
  initialState,
  reducers: {
    roomJoined(state, action: PayloadAction<{ room: string; users: string[] }>) {
      roomState(state, action.payload.room).members = [...action.payload.users];
    },

    presenceChanged(
      state,
      action: PayloadAction<{ room: string; user: string; event: 'joined' | 'left' }>,
    ) {
      const { room, user, event } = action.payload;
      const r = roomState(state, room);
      if (event === 'joined') {
        r.members.push(user);
        return;
      }
      const index = r.members.indexOf(user);
      if (index >= 0) r.members.splice(index, 1);
      if (!r.members.includes(user)) delete r.typing[user];
    },

    /** Adds our own message immediately ("optimistic"), before the server confirms it. */
    messageQueued(
      state,
      action: PayloadAction<{ room: string; cid: string; text: string; from: string; ts: string; error?: string }>,
    ) {
      const { room, cid, text, from, ts, error } = action.payload;
      const r = roomState(state, room);
      if (r.messages.some((m) => m.cid === cid)) return;
      r.messages.push({
        key: cid,
        cid,
        room,
        from,
        text,
        ts,
        status: error ? 'failed' : 'sending',
        error,
      });
    },

    messageRetrying(state, action: PayloadAction<{ room: string; cid: string }>) {
      const m = state.rooms[action.payload.room]?.messages.find(
        (x) => x.cid === action.payload.cid && x.id === undefined,
      );
      if (m) {
        m.status = 'sending';
        m.error = undefined;
      }
    },

    messageFailed(state, action: PayloadAction<{ cid: string; error: string }>) {
      for (const r of Object.values(state.rooms)) {
        const m = r.messages.find((x) => x.cid === action.payload.cid && x.id === undefined);
        if (m) {
          m.status = 'failed';
          m.error = action.payload.error;
          return;
        }
      }
    },

    /** The connection dropped: whatever was still waiting for an acknowledgement is now in doubt. */
    allPendingFailed(state, action: PayloadAction<string>) {
      for (const r of Object.values(state.rooms)) {
        for (const m of r.messages) {
          if (m.status === 'sending') {
            m.status = 'failed';
            m.error = action.payload;
          }
        }
      }
    },

    /** A message broadcast by the server (including the echo of our own). */
    messageReceived(state, action: PayloadAction<ReceivedMessage>) {
      const { id, cid, room, from, text, ts } = action.payload;
      const r = roomState(state, room);
      delete r.typing[from];

      // the acknowledgement of one of our optimistic messages
      if (cid) {
        const pending = r.messages.find((m) => m.cid === cid && m.id === undefined);
        if (pending) {
          pending.id = id;
          pending.ts = ts;
          pending.status = 'sent';
          pending.error = undefined;
          r.messages = normalize(r.messages);
          return;
        }
      }

      // already known, for example delivered both by history and live
      if (id !== undefined && r.messages.some((m) => m.id === id)) return;

      r.messages = normalize([
        ...r.messages,
        {
          key: id !== undefined ? `m${id}` : `t${ts}-${from}-${r.messages.length}`,
          id,
          cid,
          room,
          from,
          text,
          ts,
          status: 'sent',
        },
      ]);
    },

    typingStarted(state, action: PayloadAction<{ room: string; user: string; at: number }>) {
      roomState(state, action.payload.room).typing[action.payload.user] = action.payload.at;
    },

    typingExpired(state, action: PayloadAction<{ room: string; user: string; at: number }>) {
      const r = state.rooms[action.payload.room];
      // only clear it if no newer typing event arrived in the meantime
      if (r && r.typing[action.payload.user] === action.payload.at) {
        delete r.typing[action.payload.user];
      }
    },

    historyRequested(state, action: PayloadAction<{ room: string }>) {
      const r = roomState(state, action.payload.room);
      r.loadingHistory = true;
      r.historyError = null;
    },

    historyLoaded(
      state,
      action: PayloadAction<{ room: string; items: ApiMessage[]; nextCursor: number | null; older: boolean }>,
    ) {
      const { room, items, nextCursor, older } = action.payload;
      const r = roomState(state, room);
      const firstLoad = !r.loaded;
      r.loadingHistory = false;
      r.historyError = null;
      r.loaded = true;
      r.messages = mergeHistory(r.messages, items.map(fromApi));
      // a refresh of the newest page must not move the cursor of older pages we already loaded
      if (older || firstLoad) {
        r.nextCursor = nextCursor;
        r.hasMore = nextCursor !== null;
      }
    },

    historyFailed(state, action: PayloadAction<{ room: string; error: string }>) {
      const r = roomState(state, action.payload.room);
      r.loadingHistory = false;
      r.historyError = action.payload.error;
    },
  },
  extraReducers: (builder) => {
    builder.addCase(logout, () => initialState).addCase(sessionExpired, () => initialState);
  },
});

export const {
  roomJoined,
  presenceChanged,
  messageQueued,
  messageRetrying,
  messageFailed,
  allPendingFailed,
  messageReceived,
  typingStarted,
  typingExpired,
  historyRequested,
  historyLoaded,
  historyFailed,
} = chatSlice.actions;
export default chatSlice.reducer;
