export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const token = sessionStorage.getItem('infrapilot_session');
  const headers = new Headers(init?.headers);
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const response = await fetch(path, { ...init, headers });
  if (!response.ok) throw new Error(await response.text() || `Request failed (${response.status})`);
  return response.status === 204 ? (undefined as T) : response.json();
}
