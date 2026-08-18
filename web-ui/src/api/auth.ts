import { api } from './client';
import type { Discovery, Session } from '../types/api';
export const discover = () => api<Discovery>('/api/auth/discover');
export async function authenticate(deviceId: string): Promise<Session> {
  const challenge = await api<{ challenge_id: string; challenge: string }>('/api/auth/challenge', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ device_id: deviceId }) });
  const local = await fetch('http://127.0.0.1:8091/sign', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ challenge: challenge.challenge }) });
  if (!local.ok) throw new Error('Local InfraPilot Agent unavailable');
  const signed = await local.json() as { signature: string };
  const session = await api<Session>('/api/auth/verify', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ challenge_id: challenge.challenge_id, signature: signed.signature }) });
  sessionStorage.setItem('infrapilot_session', session.session); return session;
}
export const logout = () => api<void>('/api/auth/logout', { method: 'POST' }).finally(() => sessionStorage.removeItem('infrapilot_session'));
