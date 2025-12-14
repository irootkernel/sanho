import { GetKkachiState } from '@/application';
import { ApiKkachiStateRepository } from '@/data';

/**
 * Runtime contains all the dependencies for the application.
 * This allows easy testing by swapping implementations.
 */
export interface Runtime {
    /** Use case for getting kkachi state */
    getKkachiState: GetKkachiState;
    /** Function to trigger a state refresh */
    refreshState: () => Promise<void>;
}

/**
 * Creates the production runtime with real implementations.
 */
export function createRuntime(): Runtime {
    const repository = new ApiKkachiStateRepository();
    const getKkachiState = new GetKkachiState(repository);

    return {
        getKkachiState,
        refreshState: async () => {
            // Trigger a state refresh by executing the use case
            await getKkachiState.execute();
        },
    };
}
