import { useRef, useState, type ChangeEvent, type FormEvent, type KeyboardEvent } from 'react';
import { socketSend } from '../socket/actions';
import { sendMessage } from '../store/actions';
import { useAppDispatch, useAppSelector } from '../store/hooks';

const TYPING_THROTTLE_MS = 2000;

export function Composer({ room }: { room: string }) {
  const dispatch = useAppDispatch();
  const online = useAppSelector((s) => s.connection.status === 'connected');
  const [text, setText] = useState('');
  const lastTyping = useRef(0);

  const submit = () => {
    const value = text.trim();
    if (!value || !online) return;
    dispatch(sendMessage(room, value));
    setText('');
  };

  const onChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    const now = Date.now();
    if (e.target.value.trim() && online && now - lastTyping.current > TYPING_THROTTLE_MS) {
      lastTyping.current = now;
      dispatch(socketSend({ type: 'typing', room }));
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends, Shift+Enter adds a line; never send while an IME composition is active
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
    }
  };

  const onSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    submit();
  };

  return (
    <form className="composer" onSubmit={onSubmit}>
      <label htmlFor="composer-input" className="sr-only">
        Message
      </label>
      <textarea
        id="composer-input"
        value={text}
        onChange={onChange}
        onKeyDown={onKeyDown}
        rows={2}
        maxLength={2000}
        placeholder={online ? `Message #${room}` : 'Waiting for the connection…'}
      />
      <button type="submit" className="btn btn-primary" disabled={!online || !text.trim()}>
        Send
      </button>
    </form>
  );
}
