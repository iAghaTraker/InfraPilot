import { useEffect, useState } from 'react';
import { authenticate, discover } from '../api/auth';
export function Login({ onSuccess }: { onSuccess: () => void }) { const [state,setState]=useState('Searching for InfraPilot Agent...'); const [id,setId]=useState('');
  useEffect(()=>{discover().then(d=>{if(d.available){setId(d.device_id||'');setState('Device detected.');}else setState('Device not found. Run infrapilot sk create.');}).catch(()=>setState('Unable to reach the Web Panel.'));},[]);
  const login=async()=>{try{setState('Authenticating...');await authenticate(id);onSuccess();}catch(e){setState(e instanceof Error?e.message:'Device not paired.');}};
  return <main className="auth"><div className="brand">IP</div><p className="eyebrow">Infrastructure control plane</p><h1>Welcome to InfraPilot</h1><p className="muted">{state}</p>{id&&<button onClick={login}>Authenticate device</button>}</main>;
}
