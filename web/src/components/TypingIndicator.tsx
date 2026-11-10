import { EMPTY_ROOM } from '../store/chatSlice';
import { useAppSelector } from '../store/hooks';

export function TypingIndicator({ room }: { room: string }) {
  const typing = useAppSelector((s) => s.chat.rooms[room]?.typing) ?? EMPTY_ROOM.typing;
  const me = useAppSelector((s) => s.auth.user?.username);
  const names = Object.keys(typing).filter((name) => name !== me);

  let text = '';
  if (names.length === 1) text = `${names[0]} is typing…`;
  else if (names.length === 2) text = `${names[0]} and ${names[1]} are typing…`;
  else if (names.length > 2) text = 'Several people are typing…';

  // the element is always rendered, so the layout does not jump when someone starts typing
  return (
    <div className="typing" aria-live="polite">
      {text}
    </div>
  );
}
