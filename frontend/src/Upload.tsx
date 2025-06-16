import React, {type DragEvent, type ReactElement, useCallback, useEffect, useState} from "react";
import {UploadCloud} from "lucide-react";
import QRCode from "react-qr-code";
import {toNano} from "ton";
import {ToSz} from "./FileTile.tsx";

type UploadZoneProps = {
    drag: boolean;
    onDrag: (e: DragEvent<HTMLDivElement>, enter: boolean) => void;
    inputRef: React.RefObject<HTMLInputElement | null>;
    setSelectedFile : (file: File | null) => void;
    setShowModal : (show: boolean) => void;
};

export const UploadZone: React.FC<UploadZoneProps> = ({
                                                   drag,
                                                   onDrag,
                                                   inputRef,
                                                   setSelectedFile,
                                                   setShowModal
                                               }) => {
    const handleClick = () => inputRef.current?.click();

    const handleDrop = useCallback(
        (e: DragEvent<HTMLDivElement>) => {
            onDrag(e, false);
            setSelectedFile(e.dataTransfer.files[0]);
            setShowModal(true);
        },
        [onDrag]
    );

    const onInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        if (e.target.files && e.target.files.length > 0) {
            setSelectedFile(e.target.files[0]);
            setShowModal(true);
            e.target.value = "";
        }
    };

    return (
        <div
            className={`upload-zone${drag ? " drag" : ""}`}
            onDragEnter={(e) => onDrag(e, true)}
            onDragOver={(e) => onDrag(e, true)}
            onDragLeave={(e) => onDrag(e, false)}
            onDrop={handleDrop}
            onClick={handleClick}
        >
            <UploadCloud size={40} color="#0098EA" />
            <p>
                Drag & drop files here, or{" "}
                <span className="btn">
          browse
        </span>{" "}
                to upload.
            </p>
            <input
                ref={inputRef}
                type="file"
                multiple
                style={{ display: "none" }}
                onChange={onInputChange}
            />
        </div>
    );
};

interface FileUploadModalProps {
    file: File;
    onCancel: () => void;
    onUploaded: () => void; // вызывается после успешной загрузки
    uploadFile: (file: File, onProgress: (percent: number) => void) => Promise<void>;
}

export const FileUploadModal: React.FC<FileUploadModalProps> = ({
                                                             file,
                                                             onCancel,
                                                             onUploaded,
                                                             uploadFile,
                                                         }) => {
    const [progress, setProgress] = useState(0);

    useEffect(() => {
        (async () => {
            try {
                await uploadFile(file, setProgress);
                setProgress(0);
                onUploaded();
            } catch (e) {
                if (e !== null) {
                    alert("Upload failed: " + e);
                }
                onCancel();
            }
        })()
    }, [])

    return (
        <Modal onCancel={onCancel} inner={
            <>
                <h2>Uploading file</h2>
                <p><b>Name:</b> {file.name}</p>
                <p><b>Size:</b> {ToSz(file.size)}</p>
                    <div style={{ marginTop: 22 }}>
                        <div style={{ color: "#0098EA", fontWeight: 500 }}>
                            Uploading: {progress}%
                        </div>
                        <div className="progress-bar-container">
                            <div className="progress-bar" style={{ width: `${progress}%` }}/>
                        </div>
                    </div>
                <div className="modal-actions" style={{ marginTop: 28 }}>
                    <button className="modal-btn-cancel" onClick={onCancel}>
                        Cancel
                    </button>
                </div>
            </>
        }/>)
};

interface DeployModalProps {
    filePriceInfo: FileDeployInfo | null;
    onCancel: () => void;
    onDeploy: (amount: string, id: string) => void;
}

export interface FileDeployInfo {
    id: string;
    address: string;
    stateBoc: string;
    bodyBoc: string;
    pricePerDay: string;
    pricePerProof: string;
    proofPeriodEvery: string;
    proofPeriodEverySec: number;
}

export const DeployModal: React.FC<DeployModalProps> = ({
                                                            filePriceInfo,
                                                            onCancel,
                                                            onDeploy,
                                                        }) => {
    const [amount, setAmount] = useState("0.5");

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        let { value } = e.target;

        // This pattern allows digits before the dot, and up to 9 digits after the dot.
        const match = value.match(/^(\d+)(\.(\d{0,9})?)?$/);

        if (match) {
            // If there is a decimal part longer than 9, truncate it.
            const integerPart = match[1];
            const decimalPart = match[3] || "";
            value = decimalPart ? `${integerPart}.${decimalPart}` : integerPart;
            setAmount(value);
        } else if (value === "" || value === "-") {
            // Allow deletion or a starting minus sign
            setAmount(value);
        }
        // Otherwise, ignore the change if it doesn't match our expected number format.
    };

    if (!filePriceInfo) {
        return (
            <Modal onCancel={onCancel} inner={
                <>
                    <h2>Requesting provider rates...</h2>

                    <div style={{marginTop: 28, textAlign: "center"}}>
                        <div className="spinner" style={{
                            width: "40px",
                            height: "40px",
                            border: "4px solid #c6d6e3",
                            borderTop: "4px solid #0098EA",
                            borderRadius: "50%",
                            animation: "spin 1s linear infinite",
                            margin: "0 auto"
                        }}/>
                    </div>
                    <style>
                        {`
                            @keyframes spin {
                                0% { transform: rotate(0deg); }
                                100% { transform: rotate(360deg); }
                            }
                        `}
                    </style>
                    <div className="modal-actions" style={{ marginTop: 28 }}>
                        <button className="modal-btn-cancel" onClick={onCancel}>
                            Cancel
                        </button>
                    </div>
                </>
            }/>)
    }

    let rewards = Math.floor(parseFloat(amount)/parseFloat(filePriceInfo.pricePerProof));
    if (!rewards) rewards = 0;

    let days = Math.floor(rewards*(filePriceInfo.proofPeriodEverySec/86400));

    let disabled = !amount || parseFloat(amount) < 0.09;
    return (
        <Modal onCancel={onCancel} inner={
            <>
                <h2>Deploy bag contract</h2>
                
                <p><b>Storage price per day:</b> {filePriceInfo.pricePerDay} TON</p>
                <p><b>Price per reward:</b> {filePriceInfo.pricePerProof} TON</p>
                <p><b>Provider will be rewarded every:</b> {filePriceInfo.proofPeriodEvery}</p>
                <p><br/><b style={disabled ? { color: "#e24c4c"} : {}}>Min amount is 0.09 TON</b></p>

                <label style={{ marginTop: 18, marginBottom: 6, display: "block", fontWeight: 500 }}>
                    Amount to send to contract (TON):
                    <input
                        type="number"
                        min="0"
                        step="any"
                        placeholder="Enter amount"
                        value={amount}
                        onChange={handleChange}
                        style={{
                            marginTop: 8,
                            width: "100%",
                            fontSize: "1.05em",
                            padding: "7px 10px",
                            border: "1px solid #c6d6e3",
                            borderRadius: "6px",
                            outline: "none"
                        }}
                    />
                </label>
                <p><b>Enough for {days} Days</b> ({rewards} Rewards)</p>

                <div className="modal-actions" style={{ marginTop: 28 }}>
                    <button
                        className="modal-btn"
                        onClick={() => onDeploy(amount, filePriceInfo.id)}
                        disabled={disabled}
                    >
                        Deploy
                    </button>
                    <button className="modal-btn-cancel" onClick={onCancel}>
                        Cancel
                    </button>
                </div>
            </>
        }/>)
};

interface TopupModalProps {
    file: TopupFileInfo;
    onCancel: () => void;
    onConfirm: (amount: string, id: string) => void;
}

export interface TopupFileInfo {
    id: string;
    name: string;
    address: string;
}

export const TopupModal: React.FC<TopupModalProps> = ({
                                                          file,
                                                          onCancel,
                                                          onConfirm,
                                                      }) => {
    const [amount, setAmount] = useState("0.5");

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        let { value } = e.target;

        // This pattern allows digits before the dot, and up to 9 digits after the dot.
        const match = value.match(/^(\d+)(\.(\d{0,9})?)?$/);

        if (match) {
            // If there is a decimal part longer than 9, truncate it.
            const integerPart = match[1];
            const decimalPart = match[3] || "";
            value = decimalPart ? `${integerPart}.${decimalPart}` : integerPart;
            setAmount(value);
        } else if (value === "" || value === "-") {
            // Allow deletion or a starting minus sign
            setAmount(value);
        }
        // Otherwise, ignore the change if it doesn't match our expected number format.
    };

    return (
        <Modal onCancel={onCancel} inner={
            <>
                <h2>Topup contract</h2>
                <p><b>Bag:</b> {file.id}</p>
                <p><b>For file:</b> {file.name}</p>
                <p><b>Address:</b> {file.address}</p>
                <br/>
                <p>You could also scan QR and topup contract from any wallet</p>

                <div style={{ margin: "16px 0", display: "flex", justifyContent: "center" }}>
                    <QRCode value={"ton://transfer/"+file.address+"?amount="+toNano(amount)} size={140} />
                </div>

                <label style={{ marginTop: 18, marginBottom: 6, display: "block", fontWeight: 500 }}>
                    Amount to send to contract (TON):
                    <input
                        type="number"
                        min="0"
                        step="any"
                        placeholder="Enter amount"
                        value={amount}
                        onChange={handleChange}
                        style={{
                            marginTop: 8,
                            width: "100%",
                            fontSize: "1.05em",
                            padding: "7px 10px",
                            border: "1px solid #c6d6e3",
                            borderRadius: "6px",
                            outline: "none"
                        }}
                    />
                </label>

                <div className="modal-actions" style={{ marginTop: 28 }}>
                    <button
                        className="modal-btn"
                        onClick={() => onConfirm(amount, file.name)}
                        disabled={!amount || parseFloat(amount) <= 0}
                    >
                        Topup
                    </button>
                    <button className="modal-btn-cancel" onClick={onCancel}>
                        Cancel
                    </button>
                </div>
            </>
        }/>)
};

export interface ErrorModalProps {
    text: string;
    onCancel: () => void;
}

export const ErrorModal: React.FC<ErrorModalProps> = ({
                                                          text,
                                                          onCancel
                                                      }) => {

    return (
        <Modal onCancel={onCancel} inner={
            <>
                <h2>Error</h2>
                <p><b>{text}</b></p>

                <div className="modal-actions" style={{ marginTop: 28 }}>
                    <button className="modal-btn-cancel" onClick={onCancel}>
                        Cancel
                    </button>
                </div>
            </>
        }/>
    );
};

export interface ConfirmData {
    text: string;
    onConfirm: () => void;
}

export interface ConfirmModalProps {
    data: ConfirmData | null;
    onClose: () => void;
}

export const ConfirmModal: React.FC<ConfirmModalProps> = ({
                                                          data,
                                                          onClose,
                                                      }) => {
    if (!data) return null;

    return (
        <Modal onCancel={onClose} inner={
            <>
                <h2>Confirm Action</h2>
                <p><b>{data.text}</b></p>

                <div className="modal-actions" style={{ marginTop: 28 }}>
                    <button
                        className="modal-btn"
                        onClick={() => {onClose(); data.onConfirm();}}
                    >
                        Confirm
                    </button>
                    <button className="modal-btn-cancel" onClick={onClose}>
                        Cancel
                    </button>
                </div>
            </>
        }/>
    );
};

interface ModalProps {
    inner: ReactElement;
    onCancel: () => void;
}

export const Modal: React.FC<ModalProps> = ({
                                                          inner,
                                                          onCancel,
                                                      }) => {

    useEffect(() => {
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                onCancel();
            }
        };

        window.addEventListener("keydown", handleKeyDown);

        return () => {
            window.removeEventListener("keydown", handleKeyDown);
        };
    }, []);

    return (
        <div className="modal-overlay" onClick={(e) => {
            if (e.target === e.currentTarget) onCancel();
        }}>
            <div className="modal">
                {inner}
            </div>
        </div>
    );
};