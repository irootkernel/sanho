import type { Status } from '@/domain';
import { getStatusDisplay } from '@/domain';

interface StatusBadgeProps {
    status: Status;
    count?: number;
}

export function StatusBadge({ status, count }: StatusBadgeProps) {
    const statusResult = getStatusDisplay(status);
    if (!statusResult) return null; // Safety against import issues

    const { text, color } = statusResult;

    const display = count !== undefined ? `${count} ${text}` : text;
    const prefix =
        status === 'up_to_date' ? '✓ ' : status === 'unknown' ? '' : '';

    let className = 'status-badge';
    if (color === 'green') className += ' up-to-date';
    else if (color === 'orange') className += ' outdated';
    else if (color === 'gray') className += ' unknown';
    else className += ` ${color}`; // Fallback

    return (
        <span className={className}>
            {prefix}
            {display}
        </span>
    );
}
