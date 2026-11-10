import { EMPTY_ROOM } from '../store/chatSlice';
import { noticeSet } from '../store/connectionSlice';
import { useAppDispatch, useAppSelector } from '../store/hooks';
import { Composer } from './Composer';
import { ConnectionBadge } from './ConnectionBadge';
import { MessageList } from './MessageList';
import { TypingIndicator } from './TypingIndicator';

function RoomTitle({ room }: { room: string }) {
  const members = useAppSelector((s) => s.chat.rooms[room]?.members) ?? EMPTY_ROOM.members;
  const online = [...new Set(members)].sort((a, b) => a.localeCompare(b));
  return (
    <div className="room-heading">
      <h1 className="room-title"># {room}</h1>
      <p className="room-sub" title={online.join(', ')}>
        {online.length} online{online.length > 0 ? `: ${online.join(', ')}` : ''}
      </p>
    </div>
  );
}

export function RoomView({ onOpenDrawer }: { onOpenDrawer: () => void }) {
  const dispatch = useAppDispatch();
  const room = useAppSelector((s) => s.rooms.active);
  const notice = useAppSelector((s) => s.connection.notice);

  return (
    <>
      <header className="room-header">
        <button
          type="button"
          className="icon-btn menu-btn"
          onClick={onOpenDrawer}
          aria-label="Open the room list"
          aria-controls="sidebar"
        >
          ☰
        </button>
        {room ? (
          <RoomTitle room={room} />
        ) : (
          <div className="room-heading">
            <h1 className="room-title">Parley</h1>
          </div>
        )}
        <ConnectionBadge />
      </header>

      {notice && (
        <div className="banner" role="alert">
          <span>{notice}</span>
          <button type="button" className="btn btn-small" onClick={() => dispatch(noticeSet(null))}>
            Dismiss
          </button>
        </div>
      )}

      {room ? (
        <>
          <MessageList key={room} room={room} />
          <TypingIndicator room={room} />
          <Composer room={room} />
        </>
      ) : (
        <div className="empty-state">
          <p>Pick a room on the left, or create a new one.</p>
        </div>
      )}
    </>
  );
}
