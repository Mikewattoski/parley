import { createAction } from '@reduxjs/toolkit';
import type { ClientEnvelope } from './protocol';

export const socketConnect = createAction('socket/connect');
export const socketDisconnect = createAction('socket/disconnect');
export const socketJoin = createAction<string>('socket/join');
export const socketSend = createAction<ClientEnvelope>('socket/send');
