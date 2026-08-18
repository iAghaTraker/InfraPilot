export function StatusBadge({ label, tone = 'online' }: { label: string; tone?: 'online' | 'warning' | 'offline' }) { return <span className={`badge ${tone}`}>{label}</span>; }
