import { api } from './client';
import type { SystemInfo } from '../types/api';
export const getSystem = () => api<SystemInfo>('/api/system');
