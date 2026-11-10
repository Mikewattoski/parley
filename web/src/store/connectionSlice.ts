import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import { logout, sessionExpired } from './authSlice';

export type ConnectionStatus = 'idle' | 'connecting' | 'connected' | 'reconnecting';

export interface ConnectionState {
  status: ConnectionStatus;
  attempt: number; // consecutive failed attempts while reconnecting
  epoch: number; // increments on every successful (re)connect
  notice: string | null; // last server error that is not tied to a message
}

const initialState: ConnectionState = { status: 'idle', attempt: 0, epoch: 0, notice: null };

const connectionSlice = createSlice({
  name: 'connection',
  initialState,
  reducers: {
    connecting(state) {
      state.status = state.attempt > 0 ? 'reconnecting' : 'connecting';
    },
    connected(state) {
      state.status = 'connected';
      state.attempt = 0;
      state.epoch += 1;
    },
    reconnecting(state, action: PayloadAction<{ attempt: number }>) {
      state.status = 'reconnecting';
      state.attempt = action.payload.attempt;
    },
    disconnected(state) {
      state.status = 'idle';
      state.attempt = 0;
    },
    noticeSet(state, action: PayloadAction<string | null>) {
      state.notice = action.payload;
    },
  },
  extraReducers: (builder) => {
    builder.addCase(logout, () => initialState).addCase(sessionExpired, () => initialState);
  },
});

export const { connecting, connected, reconnecting, disconnected, noticeSet } = connectionSlice.actions;
export default connectionSlice.reducer;
