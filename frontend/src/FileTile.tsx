import React, {type MouseEvent} from "react";
import {Copy as CopyIcon, ExternalLink, Loader, Trash2} from "lucide-react";

export interface FileData {
    id: string;
    name: string;
    size: number;
    status: string;
    providerStatus: string;
    providerStatusReason: string;
    contractLink: string | null;
    balanceTon: string | null;
    expiryAt: number | null;
    bagId: string | null;
    pricePerDay: string | null;
}

type FileTileProps = {
    file: FileData;
    now: number;
    getFileIcon: (name?: string) => React.ElementType;
    handleDeploy: (id: string) => void;
    handleDelete: (id: string) => void;
    handleWithdraw: (id: string) => void;
    handleTopup: (id: string) => void;
};

export const FileTile: React.FC<FileTileProps> = ({
                                               file,
                                               now,
                                               getFileIcon,
                                               handleDeploy,
                                               handleDelete,
                                               handleWithdraw,
                                               handleTopup
                                           }) => {
    const remaining = file.expiryAt ? Math.max(0, file.expiryAt - now) : 0;
    const minutes = String(Math.floor(remaining / 60000)).padStart(2, "0");
    const seconds = String(Math.floor((remaining % 60000) / 1000)).padStart(2, "0");
    const timerText = `${minutes}:${seconds}`;
    const Icon = getFileIcon(file.name);

    const onCopyBagId = async (e: MouseEvent<HTMLButtonElement>) => {
        if (!file.bagId) return;
        const btn = e.currentTarget as HTMLButtonElement;
        const svg = btn.querySelector("svg");
        btn.disabled = true;
        await navigator.clipboard.writeText(file.bagId);
        const original = svg ? svg.innerHTML : "";
        if (svg) {
            svg.innerHTML =
                '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" fill="green" viewBox="0 0 24 24"><path d="M9 16.2l-4.2-4.2a1 1 0 0 1 1.4-1.4L9 13.6l8.8-8.8a1 1 0 0 1 1.4 1.4L9 16.2z"/></svg>';
        }
        setTimeout(() => {
            if (svg) svg.innerHTML = original;
            btn.disabled = false;
        }, 300);
    };

    return (
        <div className="file-tile">
            <div className="file-tile__preview">
                    <Icon color="#0098EA" size={32} />
                <div className="file-tile__name" title={file.name}>
                    {file.name}
                </div>
            </div>

            {file.status !== "stored" ? (
                <StatusWaiting
                    timerText={timerText}
                    processing={file.status === "processing" || file.status === "deploying"}
                    onDeploy={() => handleDeploy(file.id)}
                    onDelete={() => handleDelete(file.id)}
                />
            ) : (
                <StatusStored file={file} onCopyBagId={onCopyBagId} handleTopup={handleTopup} handleWithdraw={handleWithdraw}/>
            )}

            <div className="file-tile__size">
                {file.size}
            </div>
        </div>
    );
};

type StatusWaitingProps = {
    timerText: string;
    processing: boolean;
    onDeploy: () => void;
    onDelete: () => void;
};
const StatusWaiting: React.FC<StatusWaitingProps> = ({
                                                         timerText,
                                                         processing,
                                                         onDeploy,
                                                         onDelete,
                                                     }) => (
    <div className="file-tile__status file-tile__status--waiting">
        <div className="file-tile__timer">{processing ? "Processing" : "New"} · {timerText}</div>
        <p className="file-tile__timer-desc">
            This file is only visible to you and will be deleted when time expires.
            <br />
            <br />Deploy contract to store and share it until balance runs out.
        </p>
        <div className="file-tile__actions">
            <button className="btn" onClick={onDeploy} disabled={processing}>
                {processing ? <Loader size={16} /> : "Deploy"}
            </button>
            <button className="btn-del" onClick={onDelete}>
                <Trash2 color="red" size={16} />
            </button>
        </div>
    </div>
);

type StatusStoredProps = {
    file: FileData;
    onCopyBagId: (e: MouseEvent<HTMLButtonElement>) => void;
    handleWithdraw: (id: string) => void;
    handleTopup: (id: string) => void;
};

const statusText = (status: string) => {
    switch (status) {
        case "downloading":
            return "Checking";
        case "active":
            return "Active";
        case "resolving":
            return "Checking";
        case "error":
            return "Error";
        case "warning-balance":
            return "Low Balance";
        case "expired":
            return "Expired";
    }
}

const statusClass = (status: string) => {
    switch (status) {
        case "downloading":
            return "file-tile__status--stored";
        case "active":
            return "file-tile__status--stored";
        case "resolving":
            return "file-tile__status--stored";
        case "error":
            return "file-tile__status--error";
        case "warning-balance":
            return "file-tile__status--warning";
        case "expired":
            return "Expired";
    }
}

const StatusStored: React.FC<StatusStoredProps> = ({ file, onCopyBagId, handleWithdraw, handleTopup}) => (
    <div className={"file-tile__status "+statusClass(file.providerStatus)}>
        <div className="file-tile__stored-header">
            <span  style={file.providerStatusReason ? {cursor: "help"} : {}} title={file.providerStatusReason}>{statusText(file.providerStatus)}</span>
            <a href={file.contractLink || "#"} target="_blank" rel="noreferrer">
                <ExternalLink size={12} style={{ marginLeft: "4px" }} />
            </a>
        </div>
        <div className="file-tile__balance">{file.balanceTon || "—"} TON</div>
        <div className="file-tile__price-per-day">{file.pricePerDay} TON / day</div>
        <div className="file-tile__bagid">
            <span title={file.bagId ?? ""}>{file.bagId?.slice(0, 14) || "—"}…</span>
            <button onClick={onCopyBagId}>
                <CopyIcon size={12} />
            </button>
        </div>
        <div className="file-tile__actions">
            <button
                className="btn-topup"
                onClick={() => handleTopup(file.id)}
            >
                Top Up
            </button>
            <button
                className="btn-withdraw"
                onClick={() => handleWithdraw(file.id)}
            >
                Withdraw
            </button>
        </div>
    </div>
);
