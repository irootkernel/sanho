import { useState, useCallback } from 'react';

export interface ToastState {
    id: string;
    message: string;
    type: 'success' | 'danger' | 'info';
}

export function useToast() {
    const [toasts, setToasts] = useState<ToastState[]>([]);

    const showToast = useCallback((message: string, type: 'success' | 'danger' | 'info' = 'info') => {
        const id = Math.random().toString(36).substring(2, 9);
        setToasts((prev) => [...prev, { id, message, type }]);
    }, []);

    const removeToast = useCallback((id: string) => {
        setToasts((prev) => prev.filter((t) => t.id !== id));
    }, []);

    return { toasts, showToast, removeToast };
}
