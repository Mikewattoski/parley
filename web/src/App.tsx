import { useEffect } from 'react';
import { AuthScreen } from './components/AuthScreen';
import { ChatLayout } from './components/ChatLayout';
import { socketConnect, socketDisconnect } from './socket/actions';
import { useAppDispatch, useAppSelector } from './store/hooks';

export default function App() {
  const dispatch = useAppDispatch();
  const token = useAppSelector((s) => s.auth.token);

  // open the socket while logged in, close it on logout
  useEffect(() => {
    if (!token) return;
    dispatch(socketConnect());
    return () => {
      dispatch(socketDisconnect());
    };
  }, [token, dispatch]);

  return token ? <ChatLayout /> : <AuthScreen />;
}
