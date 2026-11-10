import { configureStore } from '@reduxjs/toolkit';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/client';
import { sendMessage } from '../store/actions';
import { logout } from '../store/authSlice';
import { rootReducer } from '../store/rootReducer';
import { socketConnect, socketJoin } from './actions';
import { createSocketMiddleware } from './middleware';

class FakeSocket {
  static instances: FakeSocket[] = [];

  url: string;
  readyState = 0;
  sent: string[] = [];
  closedWith: number | undefined;
  onopen: ((event: unknown) => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: unknown) => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close(code?: number) {
    this.closedWith = code;
    this.readyState = 3;
  }

  // helpers that play the part of the network
  open() {
    this.readyState = 1;
    this.onopen?.({});
  }
  receive(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) });
  }
  drop() {
    this.readyState = 3;
    this.onclose?.({});
  }
  frames() {
    return this.sent.map((s) => JSON.parse(s));
  }
}

function makeStore() {
  const fetchTicket = vi.fn(async (_token: string) => 'ticket-1');
  const store = configureStore({
    reducer: rootReducer,
    preloadedState: {
      auth: { token: 'api-token', user: { id: 1, username: 'alice' }, status: 'idle' as const, error: null },
    },
    middleware: (getDefault) =>
      getDefault().concat(createSocketMiddleware({ fetchTicket, WebSocketCtor: FakeSocket, random: () => 0 })),
  });
  return { store, fetchTicket };
}

const flush = () => vi.advanceTimersByTimeAsync(0);

async function connectedStore() {
  const ctx = makeStore();
  ctx.store.dispatch(socketJoin('general'));
  ctx.store.dispatch(socketConnect());
  await flush();
  const socket = FakeSocket.instances[0];
  socket.open();
  return { ...ctx, socket };
}

describe('socket middleware', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeSocket.instances = [];
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('connects with a fresh ticket, then joins the rooms it was asked for', async () => {
    const { store, fetchTicket } = makeStore();
    store.dispatch(socketJoin('general')); // remembered until the socket is open
    store.dispatch(socketConnect());
    expect(store.getState().connection.status).toBe('connecting');
    await flush();

    expect(fetchTicket).toHaveBeenCalledWith('api-token');
    const socket = FakeSocket.instances[0];
    expect(socket.url).toContain('ticket=ticket-1');

    socket.open();
    expect(store.getState().connection.status).toBe('connected');
    expect(socket.frames()).toEqual([{ type: 'join', room: 'general' }]);
  });

  it('reconnects with backoff and a new ticket, and rejoins its rooms', async () => {
    const { store, fetchTicket, socket } = await connectedStore();
    fetchTicket.mockResolvedValueOnce('ticket-2');

    socket.drop();
    expect(store.getState().connection.status).toBe('reconnecting');

    await vi.advanceTimersByTimeAsync(499); // first delay is 500 ms with random() = 0
    expect(FakeSocket.instances).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    await flush();

    expect(fetchTicket).toHaveBeenCalledTimes(2);
    const second = FakeSocket.instances[1];
    expect(second.url).toContain('ticket=ticket-2');

    second.open();
    expect(second.frames()).toEqual([{ type: 'join', room: 'general' }]);
    expect(store.getState().connection.status).toBe('connected');
    expect(store.getState().connection.epoch).toBe(2);
  });

  it('goes back to the login screen when the server rejects the token', async () => {
    const { store, fetchTicket } = makeStore();
    fetchTicket.mockRejectedValueOnce(new ApiError(401, 'invalid or expired token'));

    store.dispatch(socketConnect());
    await flush();

    expect(store.getState().auth.token).toBeNull();
    expect(store.getState().auth.error).toMatch(/session expired/i);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.instances).toHaveLength(0); // and it does not keep retrying
  });

  it('keeps retrying with growing delays while the server is unreachable', async () => {
    const { store, fetchTicket } = makeStore();
    fetchTicket.mockRejectedValue(new ApiError(0, 'Cannot reach the server.'));

    store.dispatch(socketConnect());
    await flush();
    expect(store.getState().connection.status).toBe('reconnecting');
    expect(fetchTicket).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(500);
    expect(fetchTicket).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1000); // the second delay is 1 s
    expect(fetchTicket).toHaveBeenCalledTimes(3);
  });

  it('confirms an optimistic message when the echo arrives, and stops the timeout', async () => {
    const { store, socket } = await connectedStore();

    store.dispatch(sendMessage('general', 'hello'));
    const frame = socket.frames().at(-1);
    expect(frame).toMatchObject({ type: 'message', room: 'general', text: 'hello' });
    expect(store.getState().chat.rooms.general.messages[0].status).toBe('sending');

    socket.receive({ type: 'message', id: 9, cid: frame.cid, room: 'general', from: 'alice', text: 'hello', ts: '2026-11-09T10:00:00Z' });
    expect(store.getState().chat.rooms.general.messages[0]).toMatchObject({ id: 9, status: 'sent' });

    await vi.advanceTimersByTimeAsync(20_000);
    expect(store.getState().chat.rooms.general.messages[0].status).toBe('sent');
  });

  it('marks a message as failed when the server rejects it', async () => {
    const { store, socket } = await connectedStore();
    store.dispatch(sendMessage('general', 'hello'));
    const frame = socket.frames().at(-1);

    socket.receive({ type: 'error', error: 'could not save message', cid: frame.cid });

    expect(store.getState().chat.rooms.general.messages[0]).toMatchObject({
      status: 'failed',
      error: 'could not save message',
    });
  });

  it('marks a message as failed when no confirmation arrives in time', async () => {
    const { store } = await connectedStore();
    store.dispatch(sendMessage('general', 'hello'));

    await vi.advanceTimersByTimeAsync(10_000);

    expect(store.getState().chat.rooms.general.messages[0]).toMatchObject({
      status: 'failed',
      error: 'No confirmation from the server.',
    });
  });

  it('fails messages that were still in flight when the connection dropped', async () => {
    const { store, socket } = await connectedStore();
    store.dispatch(sendMessage('general', 'hello'));

    socket.drop();

    expect(store.getState().chat.rooms.general.messages[0].status).toBe('failed');
  });

  it('shows server errors that are not tied to a message as a notice', async () => {
    const { store, socket } = await connectedStore();
    socket.receive({ type: 'error', error: 'rate limit exceeded, slow down' });
    expect(store.getState().connection.notice).toBe('rate limit exceeded, slow down');
  });

  it('closes the socket on logout and does not reconnect', async () => {
    const { store, socket } = await connectedStore();

    store.dispatch(logout());

    expect(socket.closedWith).toBe(1000);
    expect(store.getState().connection.status).toBe('idle');
    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.instances).toHaveLength(1);
  });
});
