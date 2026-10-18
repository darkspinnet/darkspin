//go:build scenario

package desktop

import (
	"bufio"
	"context"
	"crypto/rand"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	runbuild "github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type Options struct {
	WorkerPath    string
	GameDirectory string
	ContentPath   string
	FangPath      string
	Port          uint16
}

type Peer struct {
	option        Options
	mutex         sync.Mutex
	command       *exec.Cmd
	input         io.WriteCloser
	output        io.ReadCloser
	scanner       *bufio.Scanner
	containment   io.Closer
	log           io.Closer
	done          chan error
	token         string
	sequence      uint64
	runID         string
	paths         scenario.RunPaths
	isClosed      bool
	isReaped      bool
	runnerVersion scenario.Version
}

func New(option Options) (*Peer, error) {
	if !isPlatformSupported() {
		return nil, errors.New("scenario desktop startup currently requires Windows")
	}
	err := ValidatePort(option.Port)
	if err != nil {
		return nil, fmt.Errorf("peerPort: %w", err)
	}
	for _, path := range []string{option.WorkerPath, option.ContentPath, option.FangPath} {
		err := CheckRegular(path)
		if err != nil {
			return nil, fmt.Errorf("peerInput: %w", err)
		}
	}
	metadata, err := buildinfo.ReadFile(option.WorkerPath)
	if err != nil {
		return nil, fmt.Errorf("workerBuildInfo: %w", err)
	}
	isScenario := false
	for _, setting := range metadata.Settings {
		if setting.Key != "-tags" {
			continue
		}
		for _, tag := range strings.FieldsFunc(setting.Value, isBuildTagSeparator) {
			if tag == "scenario" {
				isScenario = true
			}
		}
	}
	if !isScenario {
		return nil, errors.New("selected launcher was not compiled with the scenario tag")
	}
	if !filepath.IsAbs(option.GameDirectory) {
		return nil, errors.New("scenario game directory must be absolute")
	}
	return &Peer{option: option}, nil
}

func isBuildTagSeparator(character rune) bool { return character == ',' || character == ' ' }

func (e *Peer) Verify(ctx context.Context, req scenario.CapabilityRequest) ([]scenario.Capability, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.command != nil || e.isClosed {
		return nil, errors.New("scenario worker cannot be reused")
	}
	start := StartRequest{RunID: req.RunID, Paths: req.Paths, Definition: req.Definition,
		GameDirectory: e.option.GameDirectory, ContentPath: e.option.ContentPath,
		FangPath: e.option.FangPath, Port: e.option.Port}
	err := ValidateStart(start)
	if err != nil {
		return nil, fmt.Errorf("peerStart: %w", err)
	}
	e.runID = req.RunID
	e.paths = req.Paths
	runnerPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("runnerExecutable: %w", err)
	}
	runnerArtifact, err := HashArtifact(ctx, "runner_binary", runnerPath)
	if err != nil {
		return nil, fmt.Errorf("runnerHash: %w", err)
	}
	e.runnerVersion = scenario.Version{Component: "runner", BuildID: runbuild.ID, Artifact: runnerArtifact}
	secret := make([]byte, 32)
	count, err := rand.Read(secret)
	if err != nil {
		return nil, fmt.Errorf("peerToken: %w", err)
	}
	if count != len(secret) {
		return nil, errors.New("short worker token randomness")
	}
	e.token = hex.EncodeToString(secret)
	command := exec.Command(e.option.WorkerPath, WorkerArgument)
	configureProcess(command)
	w, err := os.OpenFile(filepath.Join(req.Paths.ReportDirectory, "worker-stderr.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("workerLog: %w", err)
	}
	e.log = w
	command.Stderr = w
	e.input, err = command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("workerInput: %w", err)
	}
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("workerOutput: %w", err)
	}
	e.output = outputReader
	command.Stdout = outputWriter
	e.scanner = bufio.NewScanner(outputReader)
	e.scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	e.command = command
	err = command.Start()
	outputCloseErr := outputWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("workerSpawn: %w", errors.Join(err, outputCloseErr))
	}
	e.done = make(chan error, 1)
	go e.wait()
	if outputCloseErr != nil {
		return nil, fmt.Errorf("workerOutputClose: %w", outputCloseErr)
	}
	// The worker cannot start a client until it receives the first pipe request.
	e.containment, err = containProcess(command.Process)
	if err != nil {
		cleanupErr := e.abort()
		return nil, fmt.Errorf("workerContain: %w", errors.Join(err, cleanupErr))
	}
	response, err := e.exchange(ctx, Request{Operation: "start", Start: &start})
	capabilities := append([]scenario.Capability{scenario.NewCapability("runner", runbuild.ID)}, response.Capabilities...)
	if err != nil {
		return capabilities, fmt.Errorf("workerVerify: %w", err)
	}
	return capabilities, nil
}

func (e *Peer) Launch(ctx context.Context, req scenario.LaunchRequest) (scenario.LaunchResult, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	err := e.checkRun(req.RunID, req.Paths)
	if err != nil {
		return scenario.LaunchResult{}, fmt.Errorf("launchOwner: %w", err)
	}
	startupContext, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	response, err := e.exchange(startupContext, Request{Operation: "resume"})
	if err != nil {
		return scenario.LaunchResult{}, fmt.Errorf("workerResume: %w", err)
	}
	versions := append([]scenario.Version{e.runnerVersion}, response.Versions...)
	return scenario.LaunchResult{Observation: response.Observation, Versions: versions}, nil
}

func (e *Peer) Observe(ctx context.Context, req scenario.ObservationRequest) (scenario.Observation, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	err := e.checkRun(req.RunID, req.Paths)
	if err != nil {
		return scenario.Observation{}, fmt.Errorf("observeOwner: %w", err)
	}
	response, err := e.exchange(ctx, Request{Operation: "observe", Milestone: req.Milestone.Kind})
	if err != nil {
		return response.Observation, fmt.Errorf("workerObserve: %w", err)
	}
	return response.Observation, nil
}

func (e *Peer) Close(ctx context.Context, req scenario.CloseRequest) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.runID == "" || e.isClosed {
		return nil
	}
	if req.RunID != e.runID || req.Paths != e.paths {
		return errors.New("cleanup request belongs to another run")
	}
	var shutdownErr error
	if e.done != nil {
		response, err := e.exchange(ctx, Request{Operation: "close"})
		if err != nil && !errors.Is(err, io.EOF) {
			shutdownErr = fmt.Errorf("workerClose: %w", err)
		}
		if response.Error != "" && shutdownErr == nil {
			shutdownErr = errors.New(response.Error)
		}
		if err == nil {
			// Close acknowledges resource cleanup. Let the worker exit normally
			// before releasing the job that contains any remaining descendants.
			select {
			case waitErr := <-e.done:
				e.isReaped = true
				if waitErr != nil {
					shutdownErr = fmt.Errorf("workerExit: %w", waitErr)
				}
			case <-ctx.Done():
				shutdownErr = fmt.Errorf("workerExitDeadline: %w", ctx.Err())
			}
		}
	}
	cleanupErr := e.abort()
	if shutdownErr != nil || cleanupErr != nil {
		return fmt.Errorf("peerClose: %w", errors.Join(shutdownErr, cleanupErr))
	}
	return nil
}

func (e *Peer) checkRun(runID string, paths scenario.RunPaths) error {
	if e.isClosed || e.command == nil || e.done == nil {
		return errors.New("scenario worker is not active")
	}
	if runID != e.runID || paths != e.paths {
		return errors.New("worker request belongs to another run")
	}
	return nil
}

func (e *Peer) wait() { e.done <- e.command.Wait() }

type exchangeResult struct {
	response Response
	err      error
}

const observationResponseWait = 500 * time.Millisecond

func (e *Peer) exchange(ctx context.Context, req Request) (Response, error) {
	err := ctx.Err()
	if err != nil {
		cleanupErr := e.abort()
		return Response{}, fmt.Errorf("pipeContext: %w", errors.Join(err, cleanupErr))
	}
	e.sequence++
	req.Sequence = e.sequence
	req.ProtocolVersion = scenario.ProtocolVersion
	req.Token = e.token
	deadline, isDeadline := ctx.Deadline()
	if !isDeadline {
		return Response{}, errors.New("worker requests require a wall deadline")
	}
	err = ctx.Err()
	if err == nil && !time.Now().Before(deadline) {
		err = context.DeadlineExceeded
	}
	if err != nil {
		cleanupErr := e.abort()
		return Response{}, fmt.Errorf("pipeRequestDeadline: %w", errors.Join(err, cleanupErr))
	}
	req.Deadline = deadline
	results := make(chan exchangeResult, 1)
	go e.roundTrip(req, results)
	select {
	case result := <-results:
		response, responseErr := validateExchangeResponse(req, result)
		if responseErr != nil {
			cleanupErr := e.abort()
			return Response{}, fmt.Errorf("pipeResponse: %w", errors.Join(responseErr, ctx.Err(), cleanupErr))
		}
		contextErr := ctx.Err()
		if contextErr == nil && !time.Now().Before(req.Deadline) {
			contextErr = context.DeadlineExceeded
		}
		if contextErr != nil {
			if req.Operation == "observe" && errors.Is(contextErr, context.DeadlineExceeded) {
				expired, expiredErr := e.expiredObservationResponse(req, result, contextErr)
				return expired, fmt.Errorf("pipeExpired: %w", expiredErr)
			}
			cleanupErr := e.abort()
			return Response{}, fmt.Errorf("pipeResultContext: %w", errors.Join(contextErr, cleanupErr))
		}
		if response.Error != "" {
			return response, errors.New(response.Error)
		}
		return response, nil
	case <-ctx.Done():
		contextErr := ctx.Err()
		if req.Operation == "observe" && errors.Is(contextErr, context.DeadlineExceeded) {
			response, drainErr := e.drainObservationResponse(req, results, contextErr)
			return response, fmt.Errorf("pipeDrain: %w", drainErr)
		}
		cleanupErr := e.abort()
		return Response{}, fmt.Errorf("pipeDeadline: %w", errors.Join(contextErr, cleanupErr))
	}
}

func validateExchangeResponse(req Request, result exchangeResult) (Response, error) {
	if result.err != nil {
		return Response{}, fmt.Errorf("responseTransport: %w", result.err)
	}
	if result.response.ProtocolVersion != req.ProtocolVersion || result.response.Sequence != req.Sequence {
		return Response{}, errors.New("worker protocol or sequence mismatch")
	}
	return result.response, nil
}

func (e *Peer) drainObservationResponse(req Request, results <-chan exchangeResult, deadlineErr error) (Response, error) {
	// Only the existing reply may complete here; the worker retains the original
	// request deadline. This child context has already expired, so a later parent
	// cancellation is not observable through it. The reply wait stays bounded.
	replyDeadline := time.Now().Add(observationResponseWait)
	timer := time.NewTimer(time.Until(replyDeadline))
	defer timer.Stop()
	select {
	case result := <-results:
		if !time.Now().Before(replyDeadline) {
			cleanupErr := e.abort()
			return Response{}, fmt.Errorf("drainLate: %w", errors.Join(deadlineErr, cleanupErr))
		}
		response, err := e.expiredObservationResponse(req, result, deadlineErr)
		return response, fmt.Errorf("drainResponse: %w", err)
	case <-timer.C:
		cleanupErr := e.abort()
		return Response{}, fmt.Errorf("drainTimeout: %w", errors.Join(deadlineErr, cleanupErr))
	}
}

func (e *Peer) expiredObservationResponse(req Request, result exchangeResult, deadlineErr error) (Response, error) {
	response, err := validateExchangeResponse(req, result)
	if err != nil {
		cleanupErr := e.abort()
		return Response{}, fmt.Errorf("expiredResponse: %w", errors.Join(deadlineErr, err, cleanupErr))
	}
	if response.Observation.Outcome == scenario.Passed {
		response.Observation.Outcome = scenario.Inconclusive
	}
	response.Observation.SessionID = ""
	response.Observation.ZoneGeneration = 0
	response.Observation.Effective = nil
	response.Observation.Detail = "observation response retained after the original deadline: " + response.Observation.Detail
	var workerErr error
	if response.Error != "" {
		workerErr = errors.New(response.Error)
	}
	return response, fmt.Errorf("expiredDeadline: %w", errors.Join(deadlineErr, workerErr))
}

func (e *Peer) roundTrip(req Request, results chan<- exchangeResult) {
	err := json.NewEncoder(e.input).Encode(req)
	if err != nil {
		results <- exchangeResult{err: fmt.Errorf("requestWrite: %w", err)}
		return
	}
	var response Response
	if !e.scanner.Scan() {
		err = e.scanner.Err()
		if err == nil {
			err = io.EOF
		}
		results <- exchangeResult{err: fmt.Errorf("responseScan: %w", err)}
		return
	}
	decoder := json.NewDecoder(strings.NewReader(e.scanner.Text()))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&response)
	if err != nil {
		results <- exchangeResult{err: fmt.Errorf("responseRead: %w", err)}
		return
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		results <- exchangeResult{err: errors.New("worker response contains trailing JSON")}
		return
	}
	results <- exchangeResult{response: response}
}

func (e *Peer) abort() error {
	if e.isClosed {
		return nil
	}
	e.isClosed = true
	errs := []error{}
	if e.input != nil {
		err := e.input.Close()
		if err != nil && !errors.Is(err, os.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if e.containment != nil {
		errs = append(errs, e.containment.Close())
	} else if e.command != nil && e.command.Process != nil {
		err := e.command.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
	}
	if e.done != nil && !e.isReaped {
		select {
		case waitErr := <-e.done:
			e.isReaped = true
			// A killed owned worker is expected during abort. Wait still reaps it.
			if waitErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(waitErr, &exitErr) {
					errs = append(errs, waitErr)
				}
			}
		case <-time.After(2 * time.Second):
			errs = append(errs, errors.New("owned worker reap exceeded two seconds"))
		}
	}
	if e.output != nil {
		err := e.output.Close()
		if err != nil && !errors.Is(err, os.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if e.log != nil {
		errs = append(errs, e.log.Close())
	}
	err := errors.Join(errs...)
	if err != nil {
		return fmt.Errorf("abortOwned: %w", err)
	}
	return nil
}
