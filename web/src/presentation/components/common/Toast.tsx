import React, { useEffect, useState } from 'react';

export interface ToastProps {
    message: string;
    type?: 'success' | 'danger' | 'info';
    duration?: number;
    index?: number;
    onClose?: () => void;
}

export const Toast: React.FC<ToastProps> = ({ message, type = 'info', duration = 3000, index = 0, onClose }) => {
    const [visible, setVisible] = useState(true);

    useEffect(() => {
        const timer = setTimeout(() => {
            setVisible(false);
            if (onClose) onClose();
        }, duration);
        return () => clearTimeout(timer);
    }, [duration, onClose]);

    if (!visible) return null;

    const bgColor = type === 'success' ? '#d1e7dd' : type === 'danger' ? '#f8d7da' : '#e2e3e5';
    const textColor = type === 'success' ? '#0f5132' : type === 'danger' ? '#842029' : '#383d41';
    const borderColor = type === 'success' ? '#badbcc' : type === 'danger' ? '#f5c2c7' : '#d6d8db';

    // Calculate vertical position based on index (60px per toast)
    const bottom = 24 + (index * 60);

    return (
        <div style={{
            position: 'fixed', bottom: `${bottom}px`, right: '24px', zIndex: 3000,
            padding: '12px 20px', borderRadius: '8px', border: `1px solid ${borderColor}`,
            backgroundColor: bgColor, color: textColor,
            boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
            fontSize: '14px', fontWeight: '500', minWidth: '240px',
            display: 'flex', alignItems: 'center', justifyContent: 'space-between',
            animation: 'slideIn 0.3s ease-out',
            transition: 'bottom 0.3s ease-in-out'
        }}>
            <span>{message}</span>
            <button onClick={() => setVisible(false)} style={{
                background: 'none', border: 'none', color: 'inherit',
                marginLeft: '12px', cursor: 'pointer', fontSize: '18px', padding: 0,
                opacity: 0.5
            }}>&times;</button>
            <style>{`
                @keyframes slideIn {
                    from { transform: translateY(20px); opacity: 0; }
                    to { transform: translateY(0); opacity: 1; }
                }
            `}</style>
        </div>
    );
};
