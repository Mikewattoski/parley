import type { AuthResponse, HistoryPage } from '../types';

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

interface RequestOptions {
  method?: string;
  token?: string | null;
  body?: unknown;
}

function extractError(data: unknown, fallback: string): string {
  if (typeof data === 'object' && data !== null && 'error' in data) {
    const value = (data as { error: unknown }).error;
    if (typeof value === 'string') return value;
  }
  return fallback;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {};
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';
  if (options.token) headers.Authorization = `Bearer ${options.token}`;

  let res: Response;
  try {
    res = await fetch(path, {
      method: options.method ?? 'GET',
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    });
  } catch {
    throw new ApiError(0, 'Cannot reach the server. Is it running?');
  }

  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    // empty or non-JSON body
  }
  if (!res.ok) {
    throw new ApiError(res.status, extractError(data, res.statusText || 'Request failed'));
  }
  return data as T;
}

export function register(username: string, password: string): Promise<AuthResponse> {
  return request<AuthResponse>('/api/register', { method: 'POST', body: { username, password } });
}

export function login(username: string, password: string): Promise<AuthResponse> {
  return request<AuthResponse>('/api/login', { method: 'POST', body: { username, password } });
}

/** A 30-second, single-purpose token for opening one WebSocket. */
export async function fetchTicket(token: string): Promise<string> {
  const data = await request<{ ticket: string }>('/api/ws-ticket', { method: 'POST', token });
  return data.ticket;
}

export async function fetchRooms(token: string): Promise<string[]> {
  const data = await request<{ rooms: string[] }>('/api/rooms', { token });
  return data.rooms;
}

export function fetchMessages(
  token: string,
  room: string,
  options: { before?: number; limit?: number } = {},
): Promise<HistoryPage> {
  const params = new URLSearchParams();
  if (options.limit) params.set('limit', String(options.limit));
  if (options.before) params.set('before', String(options.before));
  const query = params.toString();
  return request<HistoryPage>(
    `/api/rooms/${encodeURIComponent(room)}/messages${query ? `?${query}` : ''}`,
    { token },
  );
}

export function wsUrl(ticket: string): string {
  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws';
  return `${scheme}://${window.location.host}/ws?ticket=${encodeURIComponent(ticket)}`;
}
