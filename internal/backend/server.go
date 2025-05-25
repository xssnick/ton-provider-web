package backend

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	domain    string
	maxFileSz uint64
	svc       *Service
	key       ed25519.PrivateKey
	logger    zerolog.Logger
	prf       *wallet.TonConnectVerifier
}

func Listen(key ed25519.PrivateKey, addr string, maxFileSz uint64, svc *Service, prf *wallet.TonConnectVerifier, logger zerolog.Logger) {
	s := &Server{
		key:       key,
		logger:    logger,
		maxFileSz: maxFileSz,
		svc:       svc,
		prf:       prf,
	}

	http.HandleFunc("/api/v1/login/data", s.getSignDataHandler)
	http.HandleFunc("/api/v1/provider", s.getProviderIdHandler)
	http.HandleFunc("/api/v1/login", s.loginHandler)

	http.HandleFunc("/api/v1/upload", s.authHandler(s.uploadHandler))
	http.HandleFunc("/api/v1/list", s.authHandler(s.listHandler))
	http.HandleFunc("/api/v1/deploy", s.authHandler(s.getDeployDataHandler))
	http.HandleFunc("/api/v1/withdraw", s.authHandler(s.getWithdrawDataHandler))
	http.HandleFunc("/api/v1/topup", s.authHandler(s.getTopupDataHandler))
	http.HandleFunc("/api/v1/remove", s.authHandler(s.removeHandler))

	logger.Info().Str("addr", addr).Msg("server started")
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Address   string                 `json:"address"`
		Proof     wallet.TonConnectProof `json:"proof"`
		StateInit []byte                 `json:"state_init"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.logger.Debug().Err(err).Msg("Failed to decode request body")
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	addr, err := address.ParseRawAddr(body.Address)
	if err != nil {
		http.Error(w, "Invalid address", http.StatusBadRequest)
		return
	}

	if err := s.prf.VerifyProof(r.Context(), addr, body.Proof, s.signData(), body.StateInit); err != nil {
		s.logger.Debug().Err(err).Str("addr", addr.String()).Msg("Failed to verify proof")
		http.Error(w, "Invalid proof", http.StatusBadRequest)
		return
	}

	// Generate sessionID by signing current time and address string
	timestamp := time.Now().Unix()
	sessionData := fmt.Sprintf("%d:%s", timestamp, addr.String())
	signature := ed25519.Sign(s.key, []byte(sessionData))
	sessionID := fmt.Sprintf("%x:%s", signature, sessionData)

	// Create and set the session cookie
	http.SetCookie(w, &http.Cookie{
		Name:  "session",
		Value: sessionID,
		Path:  "/",
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) authHandler(next func(http.ResponseWriter, *http.Request, *address.Address)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Validate the session ID
		parts := strings.SplitN(cookie.Value, ":", 2)
		if len(parts) != 2 {
			http.Error(w, "Invalid session format", http.StatusUnauthorized)
			return
		}

		signature, sessionData := parts[0], parts[1]

		// Recreate the signed message to verify the signature
		signedMessage := []byte(sessionData)
		sigBytes, err := hex.DecodeString(signature)
		if err != nil || !ed25519.Verify(s.key.Public().(ed25519.PublicKey), signedMessage, sigBytes) {
			http.Error(w, "Invalid session signature", http.StatusUnauthorized)
			return
		}

		// Extract and parse the session data
		dataParts := strings.SplitN(sessionData, ":", 2)
		if len(dataParts) != 2 {
			http.Error(w, "Invalid session data format", http.StatusUnauthorized)
			return
		}

		addr, err := address.ParseAddr(dataParts[1])
		if err != nil {
			http.Error(w, "Invalid address", http.StatusBadRequest)
		}

		// Proceed to the next handler
		next(w, r, addr)
	}
}

func (s *Server) removeHandler(w http.ResponseWriter, r *http.Request, addr *address.Address) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse fileName from query parameters
	query := r.URL.Query()
	fileName := query.Get("fileName")
	if fileName == "" {
		http.Error(w, "Missing 'fileName' query parameter", http.StatusBadRequest)
		return
	}

	// Attempt to remove the file using the service
	err := s.svc.RemoveFile(addr.String(), fileName)
	if err != nil {
		s.logger.Debug().Err(err).Msg("Failed to remove file")
		http.Error(w, "Failed to remove file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) getDeployDataHandler(w http.ResponseWriter, r *http.Request, addr *address.Address) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse fileName from query parameters
	query := r.URL.Query()
	fileName := query.Get("fileName")
	if fileName == "" {
		http.Error(w, "Missing 'fileName' query parameter", http.StatusBadRequest)
		return
	}

	// Retrieve deploy data from the service
	deployData, err := s.svc.GetDeployData(r.Context(), addr.String(), fileName)
	if err != nil {
		s.logger.Debug().Err(err).Msg("Failed to get deploy data")
		http.Error(w, "Failed to retrieve deploy data", http.StatusInternalServerError)
		return
	}

	// Return the deploy data as JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(deployData); err != nil {
		s.logger.Debug().Err(err).Msg("Failed to encode deploy data response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getWithdrawDataHandler(w http.ResponseWriter, r *http.Request, addr *address.Address) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse fileName from query parameters
	query := r.URL.Query()
	fileName := query.Get("fileName")
	if fileName == "" {
		http.Error(w, "Missing 'fileName' query parameter", http.StatusBadRequest)
		return
	}

	data, err := s.svc.GetWithdrawData(r.Context(), addr.String(), fileName)
	if err != nil {
		s.logger.Debug().Err(err).Msg("Failed to get deploy data")
		http.Error(w, "Failed to retrieve deploy data", http.StatusInternalServerError)
		return
	}

	// Return the deploy data as JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Debug().Err(err).Msg("Failed to encode withdraw data response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getTopupDataHandler(w http.ResponseWriter, r *http.Request, addr *address.Address) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters for additional data if needed
	query := r.URL.Query()
	fileName := query.Get("fileName")
	if fileName == "" {
		http.Error(w, "Missing 'fileName' query parameter", http.StatusBadRequest)
		return
	}

	// Retrieve topup data from the service
	data, err := s.svc.GetTopupData(r.Context(), addr.String(), fileName)
	if err != nil {
		s.logger.Debug().Err(err).Msg("Failed to get topup data")
		http.Error(w, "Failed to retrieve topup data", http.StatusInternalServerError)
		return
	}

	// Return the topup data as JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Debug().Err(err).Msg("Failed to encode topup data response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) listHandler(w http.ResponseWriter, r *http.Request, addr *address.Address) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Fetch the list of files for the user from the service
	files, err := s.svc.ListFilesByUser(addr.String())
	if err != nil {
		s.logger.Debug().Err(err).Msg("Failed to list files")
		http.Error(w, "Failed to list files", http.StatusInternalServerError)
		return
	}

	// Convert the file information to JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(files); err != nil {
		s.logger.Debug().Err(err).Msg("Failed to encode response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request, addr *address.Address) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the multipart form
	err := r.ParseMultipartForm(int64(s.maxFileSz))
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	// Retrieve the file
	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err = s.svc.StoreFile(file, addr.String(), handler.Filename); err != nil {
		http.Error(w, "Error storing the file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Handler to return data for client to sign as part of the proof
func (s *Server) getSignDataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Return the sign data as JSON response
	response := map[string]string{"data": s.signData()}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) getProviderIdHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Return the sign data as JSON response
	response := map[string]string{"id": strings.ToUpper(hex.EncodeToString(s.svc.providerKey))}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) signData() string {
	return fmt.Sprintf("auth:ton-box:%s", s.domain)
}
