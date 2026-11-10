export interface User {
  id: number;
  username: string;
}

export interface AuthResponse {
  token: string;
  user: User;
}

/** A message as returned by GET /api/rooms/{room}/messages. */
export interface ApiMessage {
  id: number;
  room: string;
  from: string;
  text: string;
  ts: string;
}

export interface HistoryPage {
  items: ApiMessage[]; // newest first
  next_cursor: number | null;
}

export type MessageStatus = 'sending' | 'sent' | 'failed';

/** A message as shown in the UI. */
export interface Message {
  key: string; // stable React key: the cid for our own messages, "m<id>" otherwise
  id?: number; // server-assigned once the message is saved
  cid?: string; // client-chosen id used to match the server's acknowledgement
  room: string;
  from: string;
  text: string;
  ts: string;
  status: MessageStatus;
  error?: string;
}
