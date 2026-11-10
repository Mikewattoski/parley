import type { Middleware } from '@reduxjs/toolkit';
import { ApiError, fetchTicket as fetchTicketApi, wsUrl } from '../api/client';
import { logout, sessionExpired } from '../store/authSlice';
import {
  allPendingFailed,
  messageFailed,
  messageReceived,
  presenceChanged,
  roomJoined,
  typingExpired,
  typingStarted,
} from '../store/chatSlice';
import { connected, connecting, disconnected, noticeSet, reconnecting } from '../store/connectionSlice';
import type { RootState } from '../store/rootReducer';
import { socketConnect, socketDisconnect, socketJoin, socketSend } from './actions';
import { backoffDelay } from './backoff';
import { parseServerEnvelope, type ClientEnvelope } from './protocol';

const OPEN = 1; // WebSocket.OPEN, kept as a constant so tests can pass a fake socket
const ACK_TIMEOUT_MS = 10_000;
const TYPING_TTL_MS = 3_000;

/* eslint-disable @typescript-eslint/no-explicit-any */
export interface SocketLike {
  readyState: number;
  onopen: ((event: any) => void) | null;
  onmessage: ((event: any) => void) | null;
  onclose: ((event: any) => void) | null;
  onerror: ((event: any) => void) | null;
  send(data: string): void;
  close(code?: number): void;
}
/* eslint-enable @typescript-eslint/no-explicit-any */

export type SocketCtor = new (url: string) => SocketLike;

export interface SocketDeps {
  WebSocketCtor: SocketCtor;
  fetchTicket: (token: string) => Promise<string>;
  random: () => number;
}

/**
 * Owns the WebSocket. Components never touch it: they dispatch actions
 * (socket/connect, socket/join, socket/send) and the server's frames come
 * back as ordinary Redux actions.
 *
 * - every connection attempt first fetches a fresh 30-second ticket
 * - unexpected closes trigger a reconnect with exponential backoff and jitter
 * - rooms that were joined are rejoined after a reconnect
 * - a message with no acknowledgement within 10 s is marked as failed
 */
export function createSocketMiddleware(overrides: Partial<SocketDeps> = {}): Middleware {
  const deps: SocketDeps = {
    WebSocketCtor: overrides.WebSocketCtor ?? (WebSocket as unknown as SocketCtor),
    fetchTicket: overrides.fetchTicket ?? fetchTicketApi,
    random: overrides.random ?? Math.random,
  };

  return (api) => {
    let ws: SocketLike | null = null;
    let running = false; // the app wants a connection
    let opening = false; // a ticket request is in flight
    let attempt = 0;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    const joined = new Set<string>();
    const ackTimers = new Map<string, ReturnType<typeof setTimeout>>();

    const clearReconnectTimer = () => {
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    };

    const clearAckTimer = (cid: string) => {
      const timer = ackTimers.get(cid);
      if (timer) {
        clearTimeout(timer);
        ackTimers.delete(cid);
      }
    };

    const clearAllAckTimers = () => {
      ackTimers.forEach((timer) => clearTimeout(timer));
      ackTimers.clear();
    };

    const startAckTimer = (cid: string) => {
      clearAckTimer(cid);
      ackTimers.set(
        cid,
        setTimeout(() => {
          ackTimers.delete(cid);
          api.dispatch(messageFailed({ cid, error: 'No confirmation from the server.' }));
        }, ACK_TIMEOUT_MS),
      );
    };

    const send = (envelope: ClientEnvelope): boolean => {
      if (!ws || ws.readyState !== OPEN) return false;
      try {
        ws.send(JSON.stringify(envelope));
        return true;
      } catch {
        return false;
      }
    };

    const handleFrame = (data: unknown) => {
      const frame = parseServerEnvelope(data);
      if (!frame) return;

      switch (frame.type) {
        case 'joined':
          api.dispatch(roomJoined({ room: frame.room, users: frame.users }));
          break;
        case 'presence':
          api.dispatch(presenceChanged({ room: frame.room, user: frame.from, event: frame.event }));
          break;
        case 'message':
          if (frame.cid) clearAckTimer(frame.cid);
          api.dispatch(
            messageReceived({
              id: frame.id,
              cid: frame.cid,
              room: frame.room,
              from: frame.from,
              text: frame.text,
              ts: frame.ts,
            }),
          );
          break;
        case 'typing': {
          const at = Date.now();
          api.dispatch(typingStarted({ room: frame.room, user: frame.from, at }));
          setTimeout(
            () => api.dispatch(typingExpired({ room: frame.room, user: frame.from, at })),
            TYPING_TTL_MS,
          );
          break;
        }
        case 'error':
          if (frame.cid) {
            clearAckTimer(frame.cid);
            api.dispatch(messageFailed({ cid: frame.cid, error: frame.error }));
          } else {
            api.dispatch(noticeSet(frame.error));
          }
          break;
        default:
          break; // welcome, left
      }
    };

    const scheduleReconnect = () => {
      if (!running) return;
      clearReconnectTimer();
      const delay = backoffDelay(attempt, deps.random);
      attempt += 1;
      api.dispatch(reconnecting({ attempt }));
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        void open();
      }, delay);
    };

    const open = async () => {
      if (!running || opening || ws) return;
      const token = (api.getState() as RootState).auth.token;
      if (!token) return;

      opening = true;
      api.dispatch(connecting());

      let ticket: string;
      try {
        ticket = await deps.fetchTicket(token);
      } catch (err) {
        opening = false;
        if (err instanceof ApiError && err.status === 401) {
          api.dispatch(sessionExpired()); // the token is no longer valid: back to the login screen
          return;
        }
        scheduleReconnect();
        return;
      }
      opening = false;
      if (!running) return; // logged out while waiting for the ticket

      const socket = new deps.WebSocketCtor(wsUrl(ticket));
      ws = socket;

      socket.onopen = () => {
        if (ws !== socket) return;
        attempt = 0;
        api.dispatch(connected());
        joined.forEach((room) => send({ type: 'join', room }));
      };
      socket.onmessage = (event) => {
        if (ws !== socket) return;
        handleFrame(event.data);
      };
      socket.onclose = () => {
        if (ws !== socket) return; // a socket we closed ourselves
        ws = null;
        clearAllAckTimers();
        api.dispatch(allPendingFailed('Connection lost before the server confirmed this message.'));
        scheduleReconnect();
      };
      socket.onerror = () => {
        // a close event always follows, and that is where reconnecting happens
      };
    };

    const onOnline = () => {
      if (running && !ws && !opening) {
        clearReconnectTimer();
        attempt = 0;
        void open();
      }
    };

    const teardown = () => {
      running = false;
      clearReconnectTimer();
      clearAllAckTimers();
      joined.clear();
      attempt = 0;
      const socket = ws;
      ws = null;
      if (socket) socket.close(1000);
      window.removeEventListener('online', onOnline);
      api.dispatch(disconnected());
    };

    return (next) => (action) => {
      if (socketConnect.match(action)) {
        if (!running) {
          running = true;
          window.addEventListener('online', onOnline);
        }
        void open();
      } else if (socketDisconnect.match(action) || logout.match(action) || sessionExpired.match(action)) {
        teardown();
      } else if (socketJoin.match(action)) {
        const room = action.payload;
        if (!joined.has(room)) {
          joined.add(room);
          send({ type: 'join', room }); // if not connected yet, onopen joins every remembered room
        }
      } else if (socketSend.match(action)) {
        const frame = action.payload;
        const sent = send(frame);
        if (frame.type === 'message') {
          if (sent) startAckTimer(frame.cid);
          else api.dispatch(messageFailed({ cid: frame.cid, error: 'Not connected.' }));
        }
      }
      return next(action);
    };
  };
}
