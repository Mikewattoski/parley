import { createAsyncThunk, createSlice } from '@reduxjs/toolkit';
import * as api from '../api/client';
import type { AuthResponse, User } from '../types';

const STORAGE_KEY = 'parley.auth';

export interface AuthState {
  token: string | null;
  user: User | null;
  status: 'idle' | 'loading';
  error: string | null;
}

function loadStored(): { token: string | null; user: User | null } {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as { token?: unknown; user?: unknown };
      if (typeof parsed.token === 'string' && typeof parsed.user === 'object' && parsed.user !== null) {
        return { token: parsed.token, user: parsed.user as User };
      }
    }
  } catch {
    // storage unavailable or corrupted: start logged out
  }
  return { token: null, user: null };
}

export function persistAuth(token: string | null, user: User | null): void {
  try {
    if (token && user) sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ token, user }));
    else sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore: the session simply will not survive a refresh
  }
}

export const authenticate = createAsyncThunk<
  AuthResponse,
  { mode: 'login' | 'register'; username: string; password: string },
  { rejectValue: string }
>('auth/authenticate', async ({ mode, username, password }, { rejectWithValue }) => {
  try {
    return mode === 'login' ? await api.login(username, password) : await api.register(username, password);
  } catch (err) {
    return rejectWithValue(err instanceof Error ? err.message : 'Something went wrong');
  }
});

const initialState: AuthState = { ...loadStored(), status: 'idle', error: null };

const authSlice = createSlice({
  name: 'auth',
  initialState,
  reducers: {
    logout(state) {
      state.token = null;
      state.user = null;
      state.status = 'idle';
      state.error = null;
    },
    sessionExpired(state) {
      state.token = null;
      state.user = null;
      state.status = 'idle';
      state.error = 'Your session expired. Please log in again.';
    },
    clearAuthError(state) {
      state.error = null;
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(authenticate.pending, (state) => {
        state.status = 'loading';
        state.error = null;
      })
      .addCase(authenticate.fulfilled, (state, action) => {
        state.status = 'idle';
        state.token = action.payload.token;
        state.user = action.payload.user;
      })
      .addCase(authenticate.rejected, (state, action) => {
        state.status = 'idle';
        state.error = action.payload ?? 'Something went wrong';
      });
  },
});

export const { logout, sessionExpired, clearAuthError } = authSlice.actions;
export default authSlice.reducer;
