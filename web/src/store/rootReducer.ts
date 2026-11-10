import { combineReducers } from '@reduxjs/toolkit';
import authReducer from './authSlice';
import chatReducer from './chatSlice';
import connectionReducer from './connectionSlice';
import roomsReducer from './roomsSlice';

export const rootReducer = combineReducers({
  auth: authReducer,
  connection: connectionReducer,
  rooms: roomsReducer,
  chat: chatReducer,
});

export type RootState = ReturnType<typeof rootReducer>;
