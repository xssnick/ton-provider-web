import React, {
    useState,
    useEffect,
    useRef,
    type DragEvent,
} from "react";
import {
    LogOut,
    File as FileIcon,
    FileText,
    FileImage,
    Video,
    Music2,
    FileArchive,
} from "lucide-react";
import "./index.css";
import {
    TonConnectButton,
    useTonAddress,
    useTonConnectUI,
    useTonWallet,
} from "@tonconnect/ui-react";
import {
    FileUploadModal,
    DeployModal,
    type FileDeployInfo,
    UploadZone,
    type TopupFileInfo,
    TopupModal
} from "./Upload.tsx";
import { Buffer } from "buffer";
import {type FileData, FileTile} from "./FileTile.tsx";
import {toNano} from "ton";

// @ts-ignore
window.Buffer = Buffer;

const Header: React.FC<{ tonConnectUI: any, num: number, provider: string, }> = ({ tonConnectUI, num, provider }) => {
    const addr = useTonAddress();
    const wallet = useTonWallet();

    return (
        <header className="header">
            <div>
                <h1>TON Provider Web</h1>
                <div className="provider-id">{provider ? "ID: "+provider : "Loading provider..."}</div>
            </div>
            <div className="user-info">
                {wallet && (
                    <>
                        <div className="address">
                            <div className="addr">{addr.slice(0, 6)}…{addr.slice(-6)}</div>
                            <div className="bal">{num} Files Stored</div>
                        </div>
                        <button className="btn" onClick={() => tonConnectUI.disconnect()}>
                            <LogOut size={16} />
                        </button>
                    </>
                )}
            </div>
        </header>
    );
};

const WelcomeScreen: React.FC = () => (
    <div className="welcome-screen">
        <div className="welcome-content">
            <h2>Welcome to TON Provider</h2>
            <p>
                Upload and store your files right in TON Storage. <br /><br />
                To access the service, connect your crypto wallet.
            </p>
            <ul className="welcome-features">
                <li>🔒 Immutable file storage</li>
                <li>⚡  Fast upload and easy access</li>
                <li>💎 Files stay available as long as the contract is funded</li>
            </ul>
            <div style={{margin: "28px auto", display: "flex", justifyContent: "center"}}>
                <TonConnectButton/>
            </div>
            <p className="welcome-tip">Click “Connect wallet” to log in</p>
        </div>
    </div>
);

const App: React.FC = () => {
    const [tonConnectUI] = useTonConnectUI();
    const [loadingCompleted, setLoadingCompleted] = useState(false);
    const [files, setFilesRaw] = useState<FileData[]>([]);
    const [deployingFiles, setDeployingFiles] = useState<string[]>([]);
    const [drag, setDrag] = useState(false);
    const [now, setNow] = useState(Date.now());
    const inputRef = useRef<HTMLInputElement>(null);
    const [providerId, setProviderId] = useState("");
    const [providerMaxSize, setProviderMaxSize] = useState(0);

    const [showModal, setShowModal] = useState(false);
    const [selectedFile, setSelectedFile] = useState<File | null>(null);

    const [deployModalVisible, setDeployModalVisible] = useState(false);
    const [deployParams, setDeployParams] = useState<FileDeployInfo | null>();

    const [topupModalVisible, setTopupModalVisible] = useState(false);
    const [topupFile, setTopupFile] = useState<TopupFileInfo | null>(null);

    const wallet = useTonWallet();

    const uploadCancelRef = useRef<() => void>(() => {});

    const handleDeployConfirm = async (amount: string, id: string) => {
        setDeployingFiles((prev) => prev.includes(id) ? prev : [...prev, id]);

        try {
            await deployContract(amount);
        } catch {
            setDeployingFiles((prev) => prev.filter((f) => f !== id));
        }

        setDeployModalVisible(false);
    };

    const handleDeployCancel = () => {
        setDeployModalVisible(false);
    };

    const handleTopupCancel = () => {
        setTopupModalVisible(false);
    };

    const setFiles = (updateFn: (files: FileData[]) => FileData[]) => {
        setFilesRaw((prevFiles) => {
            const filesList = updateFn(prevFiles);
            return filesList.map((f) => ({
                ...f,
                status: f.status !== "stored" ? (deployingFiles.includes(f.id) ? "deploying" : f.status) : "stored",
            }));
        });
    };

    useEffect(() => {
        (async () => {
            let info = await getProviderInfo();
            setProviderId(info.id);
            setProviderMaxSize(info.size);
        })()
    }, []);

    useEffect(() => {
        tonConnectUI.onStatusChange(async (w: any) => {
            if (!w) return;
            if (w.connectItems?.tonProof && "proof" in w.connectItems.tonProof) {
                try {
                    await doLogin(
                        w.account.address,
                        w.connectItems.tonProof.proof,
                        w.account.walletStateInit
                    );
                } catch (e) {
                    console.error("Failed to login: "+e);
                    await tonConnectUI.disconnect();
                }
            }
            tonConnectUI.setConnectRequestParameters(null);
        });

        tonConnectUI.setConnectRequestParameters({ state: "loading" });

        (async () => {
            const tonProofPayload = await fetchTonProofPayloadFromBackend();
            if (tonProofPayload) {
                tonConnectUI.setConnectRequestParameters({
                    state: "ready",
                    value: { tonProof: tonProofPayload },
                });
            } else {
                tonConnectUI.setConnectRequestParameters(null);
            }
        })();
    }, [tonConnectUI]);

    // Загрузка файлов
    const fetchFiles = async (): Promise<FileData[]> => {
        let fetched = await fetchUserFiles();
        if (!loadingCompleted) {
            setLoadingCompleted(true);
        }
        return fetched.map((f: any) => ({
            id: f.file_name,
            name: f.file_name,
            size: f.size,
            status: f.status,
            providerStatus: f.provider_status,
            providerStatusReason: f.provider_reason,
            contractLink: "https://tonscan.org/address/"+f.contract_addr,
            balanceTon: f.contract_balance,
            expiryAt: f.expire_at ? new Date(f.expire_at).getTime() : null,
            bagId: f.bag_id,
            pricePerDay: f.price_per_day,
        }));
    };

    const deployContract = async (amt: string) => {
        console.log("deploying contract");
        const transaction = {
            validUntil: Math.floor(Date.now() / 1000) + 90,
            messages: [
                {
                    address: deployParams!.address, // destination address
                    amount: toNano(amt).toString(),
                    stateInit: deployParams!.stateBoc,
                    payload: deployParams!.bodyBoc,
                }
            ]
        }
        await tonConnectUI.sendTransaction(transaction);
    };

    const updateFilesList = (async () => {
        let list = await fetchFiles();
        setFiles(()=> list);
    })

    useEffect(() => {
        if (wallet) {
            updateFilesList();
        }
    }, [now, wallet]);

    useEffect(() => {
        const tid = setInterval(() => setNow(Date.now()), 1000);
        return () => clearInterval(tid);
    }, []);

    useEffect(() => {
        const tid = setInterval(() => {
            setFiles((prev) =>
                prev.filter((f) => f.status !== "waiting" || (f.expiryAt ?? 0) > Date.now())
            );
        }, 1000);
        return () => clearInterval(tid);
    }, []);

    const handleDeploy = async (id: string) => {
        setDeployParams(null);

        setDeployModalVisible(true);

        let params = await getDeployParams(id);
        setDeployParams({
            id: id,
            address: params.contract_addr,
            stateBoc: params.state_init,
            bodyBoc: params.body,
            pricePerDay: params.per_day,
            pricePerProof: params.per_proof,
            proofPeriodEvery: params.proof_every,
        });
    };

    const handleDelete = async (id: string) => {
        await removeFile(id);
        setFiles((prev) => prev.filter((f) => f.id !== id));
    }

    const handleWithdraw = async (id: string) => {
        let params = await getWithdrawParams(id);

        console.log(params);
        console.log("withdraw contract for "+id);
        const transaction = {
            validUntil: Math.floor(Date.now() / 1000) + 90,
            messages: [
                {
                    address: params.contract_addr, // destination address
                    amount: toNano("0.05").toString(),
                    payload: params.body,
                }
            ]
        }

        await tonConnectUI.sendTransaction(transaction);
    }

    const handleTopupStart = async (id: string) => {
        setTopupFile({
            id: id,
            name: id,
        });
        setTopupModalVisible(true);
    }

    const handleTopup = async (amt: string, id: string) => {
        let params = await getTopupParams(id);
        console.log(params);

        console.log("topup contract for "+id);
        const transaction = {
            validUntil: Math.floor(Date.now() / 1000) + 90,
            messages: [
                {
                    address: params.contract_addr, // destination address
                    amount: toNano(amt).toString(),
                }
            ]
        }
        await tonConnectUI.sendTransaction(transaction);
        setTopupModalVisible(false);
    }

    const onDrag = (e: DragEvent<HTMLDivElement>, enter: boolean) => {
        e.preventDefault();
        e.stopPropagation();
        setDrag(enter);
    };

    const getFileIcon = (name: string = "") => {
        const ext = name.split(".").pop()?.toLowerCase() || "";
        if (/png|jpe?g|gif|bmp|tiff|webp|svg/.test(ext)) return FileImage;
        if (/mp4|mkv|webm|avi|mov|flv|wmv|video/.test(ext)) return Video;
        if (/mp3|wav|flac|aac|ogg|m4a|audio/.test(ext)) return Music2;
        if (/zip|rar|7z|tar|gz|bz2|tgz|xz/.test(ext)) return FileArchive;
        if (/txt|md|json|csv|log|ini|xml|yml|yaml|cfg/.test(ext)) return FileText;
        return FileIcon;
    };

    if (!wallet) {
        return (
            <div className="app">
                <Header tonConnectUI={tonConnectUI} num={files.length} provider={providerId} />
                <WelcomeScreen />
            </div>
        );
    }

    return (
        <div className="app">
            <Header tonConnectUI={tonConnectUI} num={files.length} provider={providerId} />

            {showModal && selectedFile && (
                <FileUploadModal
                    file={selectedFile}
                    onCancel={() => { uploadCancelRef.current(); setShowModal(false); setSelectedFile(null);  }}
                    onUploaded={async () => {
                        setShowModal(false);
                        setSelectedFile(null);
                        await updateFilesList();
                    }}
                    uploadFile={async (file, onProgress) => {
                        if (file.size > providerMaxSize) {
                            alert("File is too big. Max size is "+(providerMaxSize/1024)+" KB");
                            return;
                        }

                        const { promise, cancel } = uploadFileWithProgress(file, onProgress);
                        uploadCancelRef.current = cancel;
                        await promise;
                    }}
                />
            )}

            {deployModalVisible && (
                <DeployModal
                    filePriceInfo={deployParams!}
                    onCancel={handleDeployCancel}
                    onDeploy={handleDeployConfirm}
                />
            )}

            {topupModalVisible && (
                <TopupModal
                    file={topupFile!}
                    onCancel={handleTopupCancel}
                    onConfirm={handleTopup}
                />
            )}

            {wallet ?
                <UploadZone
                    drag={drag}
                    onDrag={onDrag}
                    inputRef={inputRef}
                    setSelectedFile={setSelectedFile}
                    setShowModal={setShowModal}
                /> : null}


                {files.length === 0 ? (
                    loadingCompleted ? (
                        <div className="files-text">
                            <p>No files yet</p>
                        </div>
                    ) : (
                        <div className="files-text">
                            <p>Loading...</p>
                        </div>
                    )
                ) : (
                    <div className="files-grid">
                        {files.map((file) => (
                            <FileTile
                                key={file.id}
                                file={file}
                                now={now}
                                getFileIcon={getFileIcon}
                                handleDeploy={handleDeploy}
                                handleDelete={handleDelete}
                                handleWithdraw={handleWithdraw}
                                handleTopup={handleTopupStart}
                            />
                        ))}
                    </div>
                )}
        </div>
    );
};

export default App;

async function doLogin(
    address: string,
    proof: string,
    stateInit: any
): Promise<any> {
    const body = { address, proof, state_init: stateInit };
    const response = await fetch("/api/v1/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
    });

    if (!response.ok) throw new Error(`Failed to login: ${response.status} ${response.statusText}`);
    return response.json();
}

async function fetchTonProofPayloadFromBackend(): Promise<any> {
    const response = await fetch("/api/v1/login/data", {
        method: "GET",
        headers: { "Content-Type": "application/json" },
    });
    if (!response.ok)
        throw new Error(`Failed to fetch TON proof payload: ${response.status} ${response.statusText}`);
    return (await response.json()).data;
}

async function fetchUserFiles(): Promise<any[]> {
    const response = await fetch(`/api/v1/list`, {
        method: "GET",
        headers: { "Content-Type": "application/json" },
    });
    if (!response.ok)
        throw new Error(`Failed to fetch user files: ${response.status} ${response.statusText}`);
    return response.json();
}

export function uploadFileWithProgress(
    file: File,
    onProgress: (percent: number) => void
): { promise: Promise<void>, cancel: () => void } {
    let xhr: XMLHttpRequest;

    const promise = new Promise<void>((resolve, reject) => {
        const formData = new FormData();
        formData.append("file", file);

        xhr = new XMLHttpRequest();
        xhr.open("POST", "/api/v1/upload");

        xhr.upload.onprogress = (event) => {
            if (event.lengthComputable) {
                const percent = Math.round((event.loaded / event.total) * 100);
                onProgress(percent);
            }
        };

        xhr.onload = () => {
            if (xhr.status >= 200 && xhr.status < 300) {
                resolve();
            } else {
                reject(
                    new Error(
                        `Failed to upload file: ${xhr.status} ${xhr.statusText} — ${xhr.responseText}`
                    )
                );
            }
        };

        xhr.onerror = () => {
            reject(new Error("Network error"));
        };

        xhr.onabort = () => {
            reject(null);
        };

        xhr.send(formData);
    });

    const cancel = () => {
        if (xhr) xhr.abort();
    };

    return { promise, cancel };
}


async function removeFile(fileName: string): Promise<void> {
    const response = await fetch(`/api/v1/remove?fileName=${encodeURIComponent(fileName)}`, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
    });

    if (!response.ok) {
        throw new Error(`Failed to remove file: ${response.status} ${response.statusText}`);
    }
}

async function getDeployParams(fileName: string): Promise<any> {
    const response = await fetch(`/api/v1/deploy?fileName=${encodeURIComponent(fileName)}`, {
        method: "GET",
        headers: {"Content-Type": "application/json"},
    });

    if (!response.ok) {
        throw new Error(`Failed get deploy data: ${response.status} ${response.statusText}`);
    }
    return response.json();
}

async function getWithdrawParams(fileName: string): Promise<any> {
    const response = await fetch(`/api/v1/withdraw?fileName=${encodeURIComponent(fileName)}`, {
        method: "GET",
        headers: {"Content-Type": "application/json"},
    });

    if (!response.ok) {
        throw new Error(`Failed get withdraw data: ${response.status} ${response.statusText}`);
    }
    return response.json();
}

async function getTopupParams(fileName: string): Promise<any> {
    const response = await fetch(`/api/v1/topup?fileName=${encodeURIComponent(fileName)}`, {
        method: "GET",
        headers: {"Content-Type": "application/json"},
    });

    if (!response.ok) {
        throw new Error(`Failed get topup data: ${response.status} ${response.statusText}`);
    }
    return response.json();
}

async function getProviderInfo(): Promise<any> {
    const response = await fetch(`/api/v1/provider`, {
        method: "GET",
        headers: {"Content-Type": "application/json"},
    });

    if (!response.ok) {
        throw new Error(`Failed get provider data: ${response.status} ${response.statusText}`);
    }
    return response.json();
}


