import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import { logout, sessionExpired } from './authSlice';
import { messageReceived } from './chatSlice';

export interface RoomsState {
  list: string[];
  active: string | null;
  loading: boolean;
  error: string | null;
  unread: Record<string, number>;
}

const initialState: RoomsState = { list: [], active: null, loading: false, error: null, unread: {} };

function addRoom(state: RoomsState, room: string) {
  if (!state.list.includes(room)) {
    state.list.push(room);
    state.list.sort((a, b) => a.localeCompare(b));
  }
}

const roomsSlice = createSlice({
  name: 'rooms',
  initialState,
  reducers: {
    roomsRequested(state) {
      state.loading = true;
      state.error = null;
    },
    roomsLoaded(state, action: PayloadAction<string[]>) {
      state.loading = false;
      for (const room of action.payload) addRoom(state, room);
    },
    roomsFailed(state, action: PayloadAction<string>) {
      state.loading = false;
      state.error = action.payload;
    },
    roomAdded(state, action: PayloadAction<string>) {
      addRoom(state, action.payload);
    },
    setActiveRoom(state, action: PayloadAction<string | null>) {
      state.active = action.payload;
      if (action.payload) delete state.unread[action.payload];
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(messageReceived, (state, action) => {
        const { room } = action.payload;
        addRoom(state, room);
        if (room !== state.active) state.unread[room] = (state.unread[room] ?? 0) + 1;
      })
      .addCase(logout, () => initialState)
      .addCase(sessionExpired, () => initialState);
  },
});

export const { roomsRequested, roomsLoaded, roomsFailed, roomAdded, setActiveRoom } = roomsSlice.actions;
export default roomsSlice.reducer;
