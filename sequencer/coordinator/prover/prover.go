package prover

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	pathLib "path"
	"strings"
	"time"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/log"

	"github.com/dghubble/sling"
)

// Proof TBD this type will be received from the proof server
type Proof struct {
	PiA      [3]*big.Int    `json:"pi_a"`
	PiB      [3][2]*big.Int `json:"pi_b"`
	PiC      [3]*big.Int    `json:"pi_c"`
	Protocol string         `json:"protocol"`
}

type bigInt big.Int

// PublicInputs are the public inputs of the proof
type PublicInputs []*big.Int

// Client is the interface to a ServerProof that calculates zk proofs
type Client interface {
	// Non-blocking
	CalculateProof(ctx context.Context, zkInputs *common.ZKInputs) error
	// Blocking.  Returns the Proof and Public Data (public inputs)
	GetProof(ctx context.Context) (*Proof, []*big.Int, error)
	// Non-Blocking
	Cancel(ctx context.Context) error
	// Blocking
	WaitReady(ctx context.Context) error
}

// StatusCode is the status string of the ProofServer
type StatusCode string

const (
	// StatusCodeAborted means prover is ready to take new proof. Previous
	// proof was aborted.
	StatusCodeAborted StatusCode = "aborted"
	// StatusCodeBusy means prover is busy computing proof.
	StatusCodeBusy StatusCode = "busy"
	// StatusCodeFailed means prover is ready to take new proof. Previous
	// proof failed
	StatusCodeFailed StatusCode = "failed"
	// StatusCodeSuccess means prover is ready to take new proof. Previous
	// proof succeeded
	StatusCodeSuccess StatusCode = "success"
	// StatusCodeUnverified means prover is ready to take new proof.
	// Previous proof was unverified
	StatusCodeUnverified StatusCode = "unverified"
	// StatusCodeUninitialized means prover is not initialized
	StatusCodeUninitialized StatusCode = "uninitialized"
	// StatusCodeUndefined means prover is in an undefined state. Most
	// likely is booting up. Keep trying
	StatusCodeUndefined StatusCode = "undefined"
	// StatusCodeInitializing means prover is initializing and not ready yet
	StatusCodeInitializing StatusCode = "initializing"
	// StatusCodeReady means prover initialized and ready to do first proof
	StatusCodeReady StatusCode = "ready"
)

// Status is the return struct for the status API endpoint
type Status struct {
	Status  StatusCode `json:"status"`
	Proof   string     `json:"proof"`
	PubData string     `json:"pubData"`
}

// IsReady returns true when the prover is ready
func (status StatusCode) IsReady() bool {
	if status == StatusCodeAborted || status == StatusCodeFailed || status == StatusCodeSuccess ||
		status == StatusCodeUnverified || status == StatusCodeReady {
		return true
	}
	return false
}

// IsInitialized returns true when the prover is initialized
func (status StatusCode) IsInitialized() bool {
	if status == StatusCodeUninitialized || status == StatusCodeUndefined ||
		status == StatusCodeInitializing {
		return false
	}
	return true
}

// ErrorServer is the return struct for an API error
type ErrorServer struct {
	Status  StatusCode `json:"status"`
	Message string     `json:"msg"`
}

type apiMethod string

const (
	// GET is an HTTP GET
	GET apiMethod = "GET"
	// POST is an HTTP POST with maybe JSON body
	POST apiMethod = "POST"
)

// ProofServerClient contains the data related to a ProofServerClient
type ProofServerClient struct {
	URL          string
	client       *sling.Sling
	pollInterval time.Duration
}

// MockClient is a mock ServerProof to be used in tests.  It doesn't calculate anything
type MockClient struct {
	counter int64
	Delay   time.Duration
}

// NewProofServerClient creates a new ServerProof
func NewProofServerClient(URL string, pollInterval time.Duration) *ProofServerClient {
	if URL[len(URL)-1] != '/' {
		URL += "/"
	}
	client := sling.New().Base(URL)
	return &ProofServerClient{URL: URL, client: client, pollInterval: pollInterval}
}

func (p *ProofServerClient) apiRequest(ctx context.Context, method apiMethod, path string,
	body interface{}, ret interface{}) error {
	path = strings.TrimPrefix(path, "/")
	var errSrv ErrorServer
	var req *http.Request
	var err error
	switch method {
	case GET:
		req, err = p.client.New().Get(path).Request()
	case POST:
		// this debug condition filters only the path "inputs" in order
		// to save the zk-inputs as pure as possible before sending
		// it to the prover
		if path == "input" {
			log.Debug("ZK-INPUT: collecting zk-inputs")
			bJSON, err := json.MarshalIndent(body, "", "  ")
			if err != nil {
				return err
			}
			n := time.Now()
			// nolint reason: hardcoded 1_000_000 is the number of nanoseconds in a
			// millisecond
			//nolint:gomnd
			filename := fmt.Sprintf("zk-inputs-debug-request-%v.%03d.json", n.Unix(), n.Nanosecond()/1_000_000)

			// tmp directory is used here because we do not have easy access to
			// the configuration at this moment, the idea in the future is to make
			// this optional and configurable.
			p := pathLib.Join("/tmp/", filename)
			log.Debugf("ZK-INPUT: saving zk-inputs json file: %s", p)
			// nolint reason: 0640 allows rw to owner and r to group
			//nolint:gosec
			if err = os.WriteFile(p, bJSON, 0640); err != nil {
				return err
			}
		}

		req, err = p.client.New().Post(path).BodyJSON(body).Request()
	default:
		return fmt.Errorf("invalid http method: %v", method)
	}
	if err != nil {
		return err
	}
	if path == "input" {
		log.Debug("ZK-INPUT: sending request to proof server")
	}
	res, err := p.client.Do(req.WithContext(ctx), ret, &errSrv)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	if !(200 <= res.StatusCode && res.StatusCode < 300) {
		return fmt.Errorf("Error: %v", errSrv.Message)
	}
	if path == "input" {
		log.Debug("ZK-INPUT: request sent successfully")
	}
	return nil
}

func (p *ProofServerClient) apiStatus(ctx context.Context) (*Status, error) {
	var status Status
	return &status, p.apiRequest(ctx, GET, "/status", nil, &status)
}

func (p *ProofServerClient) apiCancel(ctx context.Context) error {
	return p.apiRequest(ctx, POST, "/cancel", nil, nil)
}

func (p *ProofServerClient) apiInput(ctx context.Context, zkInputs *common.ZKInputs) error {
	return p.apiRequest(ctx, POST, "/input", zkInputs, nil)
}

// CalculateProof sends the *common.ZKInputs to the ServerProof to compute the
// Proof
func (p *ProofServerClient) CalculateProof(ctx context.Context, zkInputs *common.ZKInputs) error {
	return p.apiInput(ctx, zkInputs)
}

// Cancel cancels any current proof computation
func (p *ProofServerClient) Cancel(ctx context.Context) error {
	return p.apiCancel(ctx)
}

// WaitReady waits until the serverProof is ready
func (p *ProofServerClient) WaitReady(ctx context.Context) error {
	for {
		status, err := p.apiStatus(ctx)
		if err != nil {
			return err
		}
		if !status.Status.IsInitialized() {
			return fmt.Errorf("Proof Server is not initialized")
		}
		if status.Status.IsReady() {
			return nil
		}
		select {
		case <-ctx.Done():
			return common.ErrDone
		case <-time.After(p.pollInterval):
		}
	}
}

// GetProof retrieves the Proof and Public Data (public inputs) from the
// ServerProof, blocking until the proof is ready.
func (p *ProofServerClient) GetProof(ctx context.Context) (*Proof, []*big.Int, error) {
	if err := p.WaitReady(ctx); err != nil {
		return nil, nil, err
	}
	status, err := p.apiStatus(ctx)
	if err != nil {
		return nil, nil, err
	}
	if status.Status == StatusCodeSuccess {
		var proof Proof
		if err := json.Unmarshal([]byte(status.Proof), &proof); err != nil {
			return nil, nil, err
		}
		var pubInputs PublicInputs
		if err := json.Unmarshal([]byte(status.PubData), &pubInputs); err != nil {
			return nil, nil, err
		}
		return &proof, pubInputs, nil
	}
	return nil, nil, fmt.Errorf("status != %v, status = %v", StatusCodeSuccess,
		status.Status)
}
