import { configureStore } from '@reduxjs/toolkit';
import { createSocketMiddleware } from '../socket/middleware';
import { persistAuth } from './authSlice';
import { rootReducer } from './rootReducer';

export const store = configureStore({
  reducer: rootReducer,
  middleware: (getDefaultMiddleware) => getDefaultMiddleware().concat(createSocketMiddleware()),
});

// keep the session across page refreshes (sessionStorage: cleared when the tab closes)
let lastToken = store.getState().auth.token;
store.subscribe(() => {
  const { token, user } = store.getState().auth;
  if (token !== lastToken) {
    lastToken = token;
    persistAuth(token, user);
  }
});

export type AppDispatch = typeof store.dispatch;
