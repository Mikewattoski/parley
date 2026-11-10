import { useLayoutEffect, useRef } from 'react';
import { loadLatest, loadOlder, retryMessage } from '../store/actions';
import { EMPTY_ROOM } from '../store/chatSlice';
import { useAppDispatch, useAppSelector } from '../store/hooks';
import type { Message } from '../types';
import { formatTime } from '../utils';

interface ItemProps {
  message: Message;
  mine: boolean;
  showAuthor: boolean;
  canRetry: boolean;
  onRetry: () => void;
}

function MessageItem({ message, mine, showAuthor, canRetry, onRetry }: ItemProps) {
  return (
    <li className={`msg${mine ? ' mine' : ''} ${message.status}`}>
      {showAuthor && !mine && <span className="msg-author">{message.from}</span>}
      <div className="bubble">{message.text}</div>
      <div className="msg-meta">
        <time dateTime={message.ts}>{formatTime(message.ts)}</time>
        {message.status === 'sending' && <span> · Sending…</span>}
        {message.status === 'failed' && (
          <>
            <span className="fail-text"> · {message.error ?? 'Failed to send'}</span>{' '}
            <button type="button" className="link-btn" disabled={!canRetry} onClick={onRetry}>
              Retry
            </button>
          </>
        )}
      </div>
    </li>
  );
}

export function MessageList({ room }: { room: string }) {
  const dispatch = useAppDispatch();
  const chat = useAppSelector((s) => s.chat.rooms[room]) ?? EMPTY_ROOM;
  const me = useAppSelector((s) => s.auth.user?.username);
  const online = useAppSelector((s) => s.connection.status === 'connected');
  const { messages, hasMore, loadingHistory, loaded, historyError } = chat;

  const containerRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);
  const restore = useRef<{ height: number; top: number; firstId: number | undefined } | null>(null);

  const firstId = messages.find((m) => m.id !== undefined)?.id;

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const pending = restore.current;
    if (pending && firstId !== pending.firstId) {
      // older messages were added above: keep the reader at the same place
      el.scrollTop = el.scrollHeight - pending.height + pending.top;
      restore.current = null;
    } else if (pending && !loadingHistory) {
      restore.current = null; // the request ended without adding anything
    } else if (!pending && stickToBottom.current) {
      el.scrollTop = el.scrollHeight; // follow new messages only if the reader is at the bottom
    }
  }, [messages, firstId, loadingHistory]);

  const onScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
  };

  const onLoadOlder = () => {
    const el = containerRef.current;
    if (el) restore.current = { height: el.scrollHeight, top: el.scrollTop, firstId };
    void dispatch(loadOlder(room));
  };

  // announced to screen readers: the newest message from somebody else
  const last = messages[messages.length - 1];
  const announcement = loaded && last && last.from !== me ? `${last.from}: ${last.text}` : '';

  return (
    <>
      <div
        className="messages"
        ref={containerRef}
        onScroll={onScroll}
        tabIndex={0}
        aria-label={`Messages in ${room}`}
      >
        {hasMore && (
          <div className="load-older">
            <button type="button" className="btn btn-small" onClick={onLoadOlder} disabled={loadingHistory}>
              {loadingHistory ? 'Loading…' : 'Load older messages'}
            </button>
          </div>
        )}
        {historyError && (
          <p className="error-text pad" role="alert">
            {historyError}{' '}
            <button type="button" className="link-btn" onClick={() => void dispatch(loadLatest(room))}>
              Retry
            </button>
          </p>
        )}
        {!loaded && loadingHistory && <p className="muted pad">Loading messages…</p>}
        {loaded && messages.length === 0 && <p className="muted pad">No messages yet. Say hello!</p>}

        <ul className="message-list">
          {messages.map((m, i) => (
            <MessageItem
              key={m.key}
              message={m}
              mine={m.from === me}
              showAuthor={i === 0 || messages[i - 1].from !== m.from}
              canRetry={online}
              onRetry={() => m.cid && dispatch(retryMessage(room, m.cid))}
            />
          ))}
        </ul>
      </div>
      <div className="sr-only" aria-live="polite">
        {announcement}
      </div>
    </>
  );
}
