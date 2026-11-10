import type { ThunkAction, UnknownAction } from '@reduxjs/toolkit';
import { ApiError, fetchMessages, fetchRooms as fetchRoomsApi } from '../api/client';
import { socketJoin, socketSend } from '../socket/actions';
import { newCid, ROOM_NAME } from '../utils';
import { sessionExpired } from './authSlice';
import {
  historyFailed,
  historyLoaded,
  historyRequested,
  messageQueued,
  messageRetrying,
} from './chatSlice';
import type { RootState } from './rootReducer';
import { roomAdded, roomsFailed, roomsLoaded, roomsRequested, setActiveRoom } from './roomsSlice';

export type AppThunk<R = void> = ThunkAction<R, RootState, undefined, UnknownAction>;

const PAGE_SIZE = 50;

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Something went wrong';
}

export const fetchRooms = (): AppThunk<Promise<void>> => async (dispatch, getState) => {
  const token = getState().auth.token;
  if (!token) return;
  dispatch(roomsRequested());
  try {
    dispatch(roomsLoaded(await fetchRoomsApi(token)));
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      dispatch(sessionExpired());
      return;
    }
    dispatch(roomsFailed(errorMessage(err)));
  }
};

/** Fetches the newest page of a room's history (also used to catch up after a reconnect). */
export const loadLatest = (room: string): AppThunk<Promise<void>> => async (dispatch, getState) => {
  const token = getState().auth.token;
  if (!token) return;
  dispatch(historyRequested({ room }));
  try {
    const page = await fetchMessages(token, room, { limit: PAGE_SIZE });
    dispatch(historyLoaded({ room, items: page.items, nextCursor: page.next_cursor, older: false }));
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      dispatch(sessionExpired());
      return;
    }
    dispatch(historyFailed({ room, error: errorMessage(err) }));
  }
};

export const loadOlder = (room: string): AppThunk<Promise<void>> => async (dispatch, getState) => {
  const state = getState();
  const token = state.auth.token;
  const chat = state.chat.rooms[room];
  if (!token || !chat || chat.loadingHistory || chat.nextCursor === null) return;
  dispatch(historyRequested({ room }));
  try {
    const page = await fetchMessages(token, room, { before: chat.nextCursor, limit: PAGE_SIZE });
    dispatch(historyLoaded({ room, items: page.items, nextCursor: page.next_cursor, older: true }));
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      dispatch(sessionExpired());
      return;
    }
    dispatch(historyFailed({ room, error: errorMessage(err) }));
  }
};

/** Switches to a room: joins it on the socket and loads its history the first time. */
export const openRoom = (room: string): AppThunk => (dispatch, getState) => {
  dispatch(setActiveRoom(room));
  dispatch(socketJoin(room));
  const chat = getState().chat.rooms[room];
  if (!chat || !chat.loaded) void dispatch(loadLatest(room));
};

export const openDefaultRoom = (): AppThunk => (dispatch, getState) => {
  if (!getState().rooms.active) dispatch(openRoom('general'));
};

/** Returns an error message, or null when the room was created and opened. */
export const createRoom =
  (name: string): AppThunk<string | null> =>
  (dispatch) => {
    const room = name.trim();
    if (!ROOM_NAME.test(room)) return 'Room names use letters, digits, _ or - (up to 32 characters).';
    dispatch(roomAdded(room));
    dispatch(openRoom(room));
    return null;
  };

/** Re-fetches the newest messages of every room that was open, after the socket reconnected. */
export const refreshAfterReconnect = (): AppThunk => (dispatch, getState) => {
  for (const [room, chat] of Object.entries(getState().chat.rooms)) {
    if (chat.loaded) void dispatch(loadLatest(room));
  }
};

/** Optimistic send: the message appears at once and is confirmed by the server's echo. */
export const sendMessage =
  (room: string, text: string): AppThunk =>
  (dispatch, getState) => {
    const value = text.trim();
    if (!value) return;
    const state = getState();
    const online = state.connection.status === 'connected';
    const cid = newCid();
    dispatch(
      messageQueued({
        room,
        cid,
        text: value,
        from: state.auth.user?.username ?? '',
        ts: new Date().toISOString(),
        error: online ? undefined : 'Not connected.',
      }),
    );
    if (online) dispatch(socketSend({ type: 'message', room, text: value, cid }));
  };

export const retryMessage =
  (room: string, cid: string): AppThunk =>
  (dispatch, getState) => {
    const state = getState();
    const message = state.chat.rooms[room]?.messages.find((m) => m.cid === cid && m.id === undefined);
    if (!message || state.connection.status !== 'connected') return;
    dispatch(messageRetrying({ room, cid }));
    dispatch(socketSend({ type: 'message', room, text: message.text, cid }));
  };
