import { createRoot } from 'react-dom/client';
import App from './App';
import MemberDetail from './MemberDetail';
import './index.css';

// La misma aplicación sirve a la ventana principal y a las ventanas de detalle.
// El backend abre estas últimas con /?member=<clave>&name=<nombre>.
const params = new URLSearchParams(window.location.search);
const memberKey = params.get('member');
const memberName = params.get('name') ?? '';

createRoot(document.getElementById('root')!).render(
  memberKey ? (
    <MemberDetail memberKey={memberKey} initialName={memberName} />
  ) : (
    <App />
  ),
);
