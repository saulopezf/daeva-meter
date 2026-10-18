import { createRoot } from 'react-dom/client';
import App from './App';
import './index.css';

// Monta la aplicación React en el div#root de index.html.
createRoot(document.getElementById('root')!).render(<App />);
