import { useState } from 'react'; import { Login } from './pages/Login'; import { Dashboard } from './pages/Dashboard'; import './styles.css';
export default function App(){const [authenticated,setAuthenticated]=useState(Boolean(sessionStorage.getItem('infrapilot_session')));return authenticated?<Dashboard/>:<Login onSuccess={()=>setAuthenticated(true)}/>};
