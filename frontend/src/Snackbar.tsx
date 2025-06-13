// Snackbar.tsx
import React, { useEffect } from "react";

interface SnackbarProps {
    message: string;
    onClose: () => void;
}

export const Snackbar: React.FC<SnackbarProps> = ({ message, onClose }) => {
    useEffect(() => {
        if (!message) return;
        const timer = setTimeout(onClose, 4000);
        return () => clearTimeout(timer);
    }, [message]);

    if (!message) return null;

    return (
        <div className="snackbar">
            {message}
            <button className="snackbar-close" onClick={onClose}>&times;</button>
        </div>
    );
};
