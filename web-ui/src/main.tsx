import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Login } from './pages/Login';
import { Dashboard } from './pages/Dashboard';
import './styles.css';

export default function App() {
  const [authenticated, setAuthenticated] = useState(Boolean(sessionStorage.getItem('infrapilot_session')));
  return authenticated ? <Dashboard /> : <Login onSuccess={() => setAuthenticated(true)} />;
}

const root = document.getElementById('root');
if (!root) throw new Error('InfraPilot UI root element is missing');
createRoot(root).render(<App />);
