import { describe, expect, it } from 'vitest';
import reducer, {
  allPendingFailed,
  historyLoaded,
  messageFailed,
  messageQueued,
  messageReceived,
  messageRetrying,
  presenceChanged,
  type ChatState,
} from './chatSlice';

const empty: ChatState = { rooms: {} };
const TS = '2026-11-09T10:00:00.000Z';

function queue(state: ChatState, cid = 'c1', text = 'hi') {
  return reducer(state, messageQueued({ room: 'general', cid, text, from: 'alice', ts: TS }));
}

describe('chat reducer', () => {
  it('shows our message immediately as "sending"', () => {
    const state = queue(empty);
    expect(state.rooms.general.messages).toHaveLength(1);
    expect(state.rooms.general.messages[0]).toMatchObject({ key: 'c1', cid: 'c1', status: 'sending' });
    expect(state.rooms.general.messages[0].id).toBeUndefined();
  });

  it('confirms the optimistic message when the echo carries the same cid', () => {
    let state = queue(empty);
    state = reducer(state, messageReceived({ id: 7, cid: 'c1', room: 'general', from: 'alice', text: 'hi', ts: '2026-11-09T10:00:01.000Z' }));

    const messages = state.rooms.general.messages;
    expect(messages).toHaveLength(1); // confirmed in place, not duplicated
    expect(messages[0]).toMatchObject({ key: 'c1', id: 7, status: 'sent', ts: '2026-11-09T10:00:01.000Z' });
  });

  it('ignores a message whose id is already known', () => {
    let state = reducer(empty, messageReceived({ id: 3, room: 'general', from: 'bob', text: 'yo', ts: TS }));
    state = reducer(state, messageReceived({ id: 3, room: 'general', from: 'bob', text: 'yo', ts: TS }));
    expect(state.rooms.general.messages).toHaveLength(1);
  });

  it('keeps confirmed messages ordered by id, with unconfirmed ones last', () => {
    let state = queue(empty, 'c1', 'still sending');
    state = reducer(state, messageReceived({ id: 9, room: 'general', from: 'bob', text: 'later', ts: TS }));
    state = reducer(state, messageReceived({ id: 5, room: 'general', from: 'bob', text: 'earlier', ts: TS }));

    expect(state.rooms.general.messages.map((m) => m.text)).toEqual(['earlier', 'later', 'still sending']);
  });

  it('marks a message failed by cid and lets it be retried', () => {
    let state = queue(empty);
    state = reducer(state, messageFailed({ cid: 'c1', error: 'could not save message' }));
    expect(state.rooms.general.messages[0]).toMatchObject({ status: 'failed', error: 'could not save message' });

    state = reducer(state, messageRetrying({ room: 'general', cid: 'c1' }));
    expect(state.rooms.general.messages[0]).toMatchObject({ status: 'sending' });
    expect(state.rooms.general.messages[0].error).toBeUndefined();
  });

  it('only fails the messages that are still waiting when the connection drops', () => {
    let state = queue(empty, 'c1', 'waiting');
    state = reducer(state, messageReceived({ id: 1, room: 'general', from: 'bob', text: 'done', ts: TS }));
    state = reducer(state, allPendingFailed('Connection lost'));

    const byText = Object.fromEntries(state.rooms.general.messages.map((m) => [m.text, m.status]));
    expect(byText).toEqual({ waiting: 'failed', done: 'sent' });
  });

  it('drops a local unconfirmed message when history already contains it', () => {
    let state = queue(empty, 'c1', 'hello');
    state = reducer(
      state,
      historyLoaded({
        room: 'general',
        older: false,
        nextCursor: null,
        items: [
          { id: 5, room: 'general', from: 'alice', text: 'hello', ts: TS },
          { id: 4, room: 'general', from: 'bob', text: 'earlier', ts: TS },
        ],
      }),
    );

    expect(state.rooms.general.messages.map((m) => [m.id, m.text])).toEqual([
      [4, 'earlier'],
      [5, 'hello'],
    ]);
  });

  it('does not move the older-pages cursor when the newest page is refreshed', () => {
    const page = (id: number) => ({ id, room: 'general', from: 'bob', text: `m${id}`, ts: TS });
    let state = reducer(empty, historyLoaded({ room: 'general', older: false, nextCursor: 50, items: [page(60)] }));
    state = reducer(state, historyLoaded({ room: 'general', older: true, nextCursor: 20, items: [page(30)] }));
    state = reducer(state, historyLoaded({ room: 'general', older: false, nextCursor: 50, items: [page(61)] })); // reconnect refresh

    expect(state.rooms.general.nextCursor).toBe(20);
  });

  it('counts connections, so a second tab does not hide the user', () => {
    let state = reducer(empty, presenceChanged({ room: 'general', user: 'bob', event: 'joined' }));
    state = reducer(state, presenceChanged({ room: 'general', user: 'bob', event: 'joined' }));
    state = reducer(state, presenceChanged({ room: 'general', user: 'bob', event: 'left' }));
    expect(state.rooms.general.members).toEqual(['bob']);

    state = reducer(state, presenceChanged({ room: 'general', user: 'bob', event: 'left' }));
    expect(state.rooms.general.members).toEqual([]);
  });
});
