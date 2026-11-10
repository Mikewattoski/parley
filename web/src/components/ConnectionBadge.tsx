import type { ConnectionStatus } from '../store/connectionSlice';
import { useAppSelector } from '../store/hooks';

const LABELS: Record<ConnectionStatus, string> = {
  idle: 'Offline',
  connecting: 'Connecting…',
  connected: 'Connected',
  reconnecting: 'Reconnecting…',
};

export function ConnectionBadge() {
  const { status, attempt } = useAppSelector((s) => s.connection);
  return (
    <span className={`status status-${status}`} role="status">
      <span className="dot" aria-hidden="true" />
      {LABELS[status]}
      {status === 'reconnecting' && attempt > 1 ? ` (attempt ${attempt})` : ''}
    </span>
  );
}
