import React from 'react';
import { ConsoleList } from '../components/terminal/ConsoleList';
import { TerminalPane } from '../components/terminal/TerminalPane';

export const TerminalPage: React.FC = () => {
    return (
        <div className="container-fluid py-4" style={{ height: 'calc(100vh - 100px)' }}>
            <div className="row h-100">
                <div className="col-md-3 h-100 border-end overflow-auto">
                    <ConsoleList />
                </div>
                <div className="col-md-9 h-100 d-flex flex-column">
                    <TerminalPane />
                </div>
            </div>
        </div>
    );
};

export default TerminalPage;
