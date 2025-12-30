import {
    useSensor,
    useSensors,
    MouseSensor,
    TouchSensor,
    KeyboardSensor,
} from '@dnd-kit/core';
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable';

/**
 * Returns a set of sensors optimized for both mouse/touch and keyboard accessibility.
 * - Mouse/Touch: Activation constraint of 5px movement to prevent accidental drags on click.
 * - Keyboard: Uses sortable keyboard coordinates for list reordering.
 */
export function useCommonSensors() {
    return useSensors(
        useSensor(MouseSensor, {
            activationConstraint: {
                distance: 5,
            },
        }),
        useSensor(TouchSensor, {
            activationConstraint: {
                delay: 250,
                tolerance: 5,
            },
        }),
        useSensor(KeyboardSensor, {
            coordinateGetter: sortableKeyboardCoordinates,
        })
    );
}
