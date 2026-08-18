export function Loading({ label = 'Loading data...' }: { label?: string }) { return <div className="state"><span className="spinner" aria-hidden="true"/><span>{label}</span></div>; }
