import { useState, type FormEvent } from 'react';
import { createRoom, openRoom } from '../store/actions';
import { logout } from '../store/authSlice';
import { useAppDispatch, useAppSelector } from '../store/hooks';

export function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  const dispatch = useAppDispatch();
  const { list, active, unread, loading, error } = useAppSelector((s) => s.rooms);
  const user = useAppSelector((s) => s.auth.user);
  const [newRoom, setNewRoom] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  const open = (room: string) => {
    dispatch(openRoom(room));
    onNavigate();
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const problem = dispatch(createRoom(newRoom));
    setFormError(problem);
    if (!problem) {
      setNewRoom('');
      onNavigate();
    }
  };

  return (
    <aside id="sidebar" className="sidebar" aria-label="Rooms">
      <div className="sidebar-head">
        <span className="brand">💬 Parley</span>
      </div>

      <nav className="room-nav" aria-label="Room list">
        {loading && list.length === 0 && <p className="muted pad">Loading rooms…</p>}
        {error && (
          <p className="error-text pad" role="alert">
            {error}
          </p>
        )}
        <ul className="room-list">
          {list.map((room) => (
            <li key={room}>
              <button
                type="button"
                className={`room-btn${room === active ? ' active' : ''}`}
                aria-current={room === active ? 'page' : undefined}
                onClick={() => open(room)}
              >
                <span className="room-name"># {room}</span>
                {unread[room] ? (
                  <span className="badge" aria-label={`${unread[room]} unread messages`}>
                    {unread[room]}
                  </span>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
      </nav>

      <form className="new-room" onSubmit={submit}>
        <label htmlFor="new-room-input" className="sr-only">
          New room name
        </label>
        <div className="new-room-row">
          <input
            id="new-room-input"
            value={newRoom}
            onChange={(e) => setNewRoom(e.target.value)}
            placeholder="New room"
            maxLength={32}
            autoComplete="off"
          />
          <button type="submit" className="btn btn-small" disabled={!newRoom.trim()}>
            Join
          </button>
        </div>
        {formError && (
          <p className="error-text" role="alert">
            {formError}
          </p>
        )}
      </form>

      <div className="sidebar-foot">
        <span className="me" title={user?.username}>
          {user?.username}
        </span>
        <button type="button" className="btn btn-small" onClick={() => dispatch(logout())}>
          Log out
        </button>
      </div>
    </aside>
  );
}
