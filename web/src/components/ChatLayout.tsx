import { useEffect, useState } from 'react';
import { fetchRooms, openDefaultRoom, refreshAfterReconnect } from '../store/actions';
import { useAppDispatch, useAppSelector } from '../store/hooks';
import { RoomView } from './RoomView';
import { Sidebar } from './Sidebar';

export function ChatLayout() {
  const dispatch = useAppDispatch();
  const epoch = useAppSelector((s) => s.connection.epoch);
  const [drawerOpen, setDrawerOpen] = useState(false);

  useEffect(() => {
    void dispatch(fetchRooms());
    dispatch(openDefaultRoom());
  }, [dispatch]);

  // after a reconnect (epoch 2 and up), catch up on messages missed while offline
  useEffect(() => {
    if (epoch > 1) dispatch(refreshAfterReconnect());
  }, [epoch, dispatch]);

  // Escape closes the room drawer on small screens
  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDrawerOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [drawerOpen]);

  return (
    <div className={`app${drawerOpen ? ' drawer-open' : ''}`}>
      <Sidebar onNavigate={() => setDrawerOpen(false)} />
      <div className="scrim" onClick={() => setDrawerOpen(false)} aria-hidden="true" />
      <main className="main">
        <RoomView onOpenDrawer={() => setDrawerOpen(true)} />
      </main>
    </div>
  );
}
