import React, {type DragEvent, useCallback, useEffect, useState} from "react";
import {UploadCloud} from "lucide-react";

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
                <span className="btn" onClick={handleClick}>
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
                alert("Upload failed: " + e);
                onCancel();
            }
        })()
    }, [])

    return (
        <div className="modal-overlay">
            <div className="modal">
                <h2>Uploading file</h2>
                <p><b>Name:</b> {file.name}</p>
                <p><b>Size:</b> {(file.size / 1024).toFixed(1)} KB</p>
                    <div style={{ marginTop: 22 }}>
                        <div style={{ color: "#0098EA", fontWeight: 500 }}>
                            Uploading: {progress}%
                        </div>
                        <div className="progress-bar-container">
                            <div className="progress-bar" style={{ width: `${progress}%` }}/>
                        </div>
                    </div>
            </div>
        </div>
    );
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
}

export const DeployModal: React.FC<DeployModalProps> = ({
                                                            filePriceInfo,
                                                            onCancel,
                                                            onDeploy,
                                                        }) => {
    const [amount, setAmount] = useState("0.5");

    if (!filePriceInfo) {
        return (
            <div className="modal-overlay">
                <div className="modal">
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
                </div>
            </div>
        );
    }
    
    return (
        <div className="modal-overlay">
            <div className="modal">
                <h2>Deploy bag</h2>
                
                <p><b>Storage price per day:</b> {filePriceInfo.pricePerDay} TON</p>
                <p><b>Price per proof:</b> {filePriceInfo.pricePerProof} TON</p>
                <p><b>Proof will be given every:</b> {filePriceInfo.proofPeriodEvery}</p>
                <p><br/><b>Min amount is 0.09 TON</b></p>

                <label style={{ marginTop: 18, marginBottom: 6, display: "block", fontWeight: 500 }}>
                    Amount to send to contract (TON):
                    <input
                        type="number"
                        min="0"
                        step="any"
                        placeholder="Enter amount"
                        value={amount}
                        onChange={e => setAmount(e.target.value)}
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
                        onClick={() => onDeploy(amount, filePriceInfo.id)}
                        disabled={!amount || parseFloat(amount) < 0.09}
                    >
                        Deploy
                    </button>
                    <button className="modal-btn-cancel" onClick={onCancel}>
                        Cancel
                    </button>
                </div>
            </div>
        </div>
    );
};

interface TopupModalProps {
    file: TopupFileInfo;
    onCancel: () => void;
    onConfirm: (amount: string, id: string) => void;
}

export interface TopupFileInfo {
    id: string;
    name: string;
}

export const TopupModal: React.FC<TopupModalProps> = ({
                                                          file,
                                                          onCancel,
                                                          onConfirm,
                                                        }) => {
    const [amount, setAmount] = useState("0.5");

    return (
        <div className="modal-overlay">
            <div className="modal">
                <h2>Topup contract</h2>
                <p><b>For file:</b> {file.name}</p>

                <label style={{ marginTop: 18, marginBottom: 6, display: "block", fontWeight: 500 }}>
                    Amount to send to contract (TON):
                    <input
                        type="number"
                        min="0"
                        step="any"
                        placeholder="Enter amount"
                        value={amount}
                        onChange={e => setAmount(e.target.value)}
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
                        onClick={() => onConfirm(amount, file.id)}
                        disabled={!amount || parseFloat(amount) <= 0}
                    >
                        Topup
                    </button>
                    <button className="modal-btn-cancel" onClick={onCancel}>
                        Cancel
                    </button>
                </div>
            </div>
        </div>
    );
};