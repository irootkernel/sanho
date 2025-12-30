import React, { useState, useEffect, useRef } from 'react';

interface RenameSessionModalProps {
    isOpen: boolean;
    initialTitle: string;
    onClose: () => void;
    onRename: (newTitle: string) => void;
}

export const RenameSessionModal: React.FC<RenameSessionModalProps> = ({
    isOpen,
    initialTitle,
    onClose,
    onRename,
}) => {
    const [title, setTitle] = useState(initialTitle);
    const inputRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        if (isOpen) {
            // Focus input after render
            setTimeout(() => {
                inputRef.current?.focus();
                inputRef.current?.select();
            }, 50);
        }
    }, [isOpen]);

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (title.trim()) {
            onRename(title.trim());
            onClose();
        }
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Escape') {
            onClose();
        }
    };

    if (!isOpen) return null;

    return (
        <div
            role="dialog"
            aria-modal="true"
            style={{
                position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
                backgroundColor: 'rgba(0, 0, 0, 0.4)', zIndex: 2000,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                backdropFilter: 'blur(2px)'
            }}
            onClick={onClose}
            onKeyDown={handleKeyDown}
        >
            <div style={{
                width: '100%', maxWidth: '400px',
                backgroundColor: '#ffffff', borderRadius: '8px',
                boxShadow: '0 20px 50px rgba(0,0,0,0.15)',
                overflow: 'hidden',
                animation: 'fadeIn 0.2s ease-out'
            }} onClick={e => e.stopPropagation()}>
                
                <div style={{ padding: '16px 20px', borderBottom: '1px solid #eee', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <h5 style={{ margin: 0, fontSize: '16px', fontWeight: '600', color: '#212529' }}>Rename Session</h5>
                    <button onClick={onClose} style={{ background: 'none', border: 'none', color: '#ccc', cursor: 'pointer', fontSize: '20px', padding: 0 }}>&times;</button>
                </div>

                <form onSubmit={handleSubmit} style={{ padding: '20px' }}>
                    <div style={{ marginBottom: '20px' }}>
                        <label style={{ display: 'block', marginBottom: '8px', fontSize: '13px', color: '#495057', fontWeight: '500' }}>Session Name</label>
                        <input
                            ref={inputRef}
                            type="text"
                            value={title}
                            onChange={(e) => setTitle(e.target.value)}
                            style={{
                                width: '100%', padding: '10px 12px',
                                border: '1px solid #dee2e6', borderRadius: '6px',
                                fontSize: '14px', outline: 'none',
                                boxShadow: 'inset 0 1px 2px rgba(0,0,0,0.05)'
                            }}
                        />
                    </div>
                    
                    <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
                        <button 
                            type="button" 
                            onClick={onClose}
                            style={{
                                padding: '8px 16px', borderRadius: '6px', border: '1px solid #dee2e6',
                                backgroundColor: '#f8f9fa', color: '#495057', fontSize: '13px', fontWeight: '500', cursor: 'pointer'
                            }}
                        >
                            Cancel
                        </button>
                        <button 
                            type="submit"
                            style={{
                                padding: '8px 16px', borderRadius: '6px', border: 'none',
                                backgroundColor: '#0d6efd', color: '#fff', fontSize: '13px', fontWeight: '500', cursor: 'pointer'
                            }}
                        >
                            Save
                        </button>
                    </div>
                </form>
            </div>
            <style>{`
                @keyframes fadeIn {
                    from { opacity: 0; transform: scale(0.95); }
                    to { opacity: 1; transform: scale(1); }
                }
            `}</style>
        </div>
    );
};
