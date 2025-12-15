import { createContext } from 'react';
import type { Runtime } from './createRuntime';

export const RuntimeContext = createContext<Runtime | null>(null);

