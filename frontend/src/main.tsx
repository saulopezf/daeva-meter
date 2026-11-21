import { createRoot } from 'react-dom/client';
import App from './App';
import MemberDetail from './MemberDetail';
import type { Metric } from './types';
import './index.css';

// La misma aplicación sirve a la ventana principal y a las ventanas de detalle.
// El backend abre estas últimas con
// /?member=<clave>&name=<nombre>&encounter=<id>&metric=<métrica>.
const params = new URLSearchParams(window.location.search);
const memberKey = params.get('member');
const memberName = params.get('name') ?? '';
const metricParam = params.get('metric');
const metric: Metric =
  metricParam === 'healing' || metricParam === 'taken' ? metricParam : 'damage';
const encounterID = Number(params.get('encounter') ?? '0') || 0;

createRoot(document.getElementById('root')!).render(
  memberKey ? (
    <MemberDetail
      memberKey={memberKey}
      initialName={memberName}
      encounterID={encounterID}
      metric={metric}
    />
  ) : (
    <App />
  ),
);
