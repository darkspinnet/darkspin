//go:build scenario

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type scenarioWorker struct {
	ctx          context.Context
	cancel       context.CancelFunc
	token        string
	sequence     uint64
	start        *desktop.StartRequest
	host         *scenarioHost
	client       *scenarioClient
	capabilities []scenario.Capability
	versions     []scenario.Version
	isResumed    bool
}

type scenarioPipeRequest struct {
	request desktop.Request
	err     error
}

func runScenarioWorker(arguments []string) (bool, error) {
	if len(arguments) != 1 || arguments[0] != desktop.WorkerArgument {
		return false, nil
	}
	err := ensureStandardUser()
	if err != nil {
		return true, fmt.Errorf("workerPrivilege: %w", err)
	}
	for _, r := range []*os.File{os.Stdin, os.Stdout} {
		fi, statErr := r.Stat()
		if statErr != nil {
			return true, fmt.Errorf("workerPipeStat: %w", statErr)
		}
		if fi.Mode()&os.ModeNamedPipe == 0 {
			return true, errors.New("scenario worker requires inherited private pipes")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := &scenarioWorker{ctx: ctx, cancel: cancel}
	requests := make(chan scenarioPipeRequest, 1)
	go readScenarioRequests(ctx, cancel, requests)
	err = worker.serve(requests)
	cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	cleanupErr := worker.close(cleanupContext)
	cleanupCancel()
	if err != nil || cleanupErr != nil {
		return true, fmt.Errorf("workerServe: %w", errors.Join(err, cleanupErr))
	}
	return true, nil
}

func readScenarioRequests(ctx context.Context, cancel context.CancelFunc, requests chan<- scenarioPipeRequest) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for scanner.Scan() {
		var req desktop.Request
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&req)
		if err == nil {
			var extra any
			trailingErr := decoder.Decode(&extra)
			if !errors.Is(trailingErr, io.EOF) {
				err = errors.New("worker request contains trailing JSON")
			}
		}
		select {
		case requests <- scenarioPipeRequest{request: req, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			cancel()
			return
		}
	}
	err := scanner.Err()
	if err != nil {
		select {
		case requests <- scenarioPipeRequest{err: fmt.Errorf("workerRead: %w", err)}:
		default:
		}
	}
	// EOF cancels lifetime even while a startup or observation is blocked.
	cancel()
}

func (e *scenarioWorker) serve(requests <-chan scenarioPipeRequest) error {
	for {
		select {
		case <-e.ctx.Done():
			return nil
		case item := <-requests:
			if item.err != nil {
				return fmt.Errorf("workerRequest: %w", item.err)
			}
			req := item.request
			err := e.validate(req)
			if err != nil {
				return fmt.Errorf("workerEnvelope: %w", err)
			}
			requestContext, cancel := context.WithDeadline(e.ctx, req.Deadline)
			response, operationErr := e.execute(requestContext, req)
			cancel()
			response.ProtocolVersion = scenario.ProtocolVersion
			response.Sequence = req.Sequence
			if operationErr != nil {
				response.Error = operationErr.Error()
			}
			err = json.NewEncoder(os.Stdout).Encode(response)
			if err != nil {
				return fmt.Errorf("workerResponse: %w", err)
			}
			if req.Operation == "close" {
				return operationErr
			}
			// A failed start may still own a server or suspended client. Remain
			// available for the parent's bounded close request after reporting it.
		}
	}
}

func (e *scenarioWorker) validate(req desktop.Request) error {
	if req.ProtocolVersion != scenario.ProtocolVersion || req.Sequence != e.sequence+1 {
		return errors.New("worker protocol or sequence mismatch")
	}
	if len(req.Token) != 64 {
		return errors.New("invalid worker token")
	}
	if e.sequence == 0 {
		if req.Operation != "start" || req.Start == nil {
			return errors.New("first worker request must start a fresh run")
		}
		e.token = req.Token
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(e.token)) != 1 {
		return errors.New("worker token mismatch")
	}
	if req.Deadline.IsZero() || !req.Deadline.After(time.Now()) || req.Deadline.After(time.Now().Add(10*time.Minute)) {
		return errors.New("worker wall deadline is invalid")
	}
	e.sequence = req.Sequence
	return nil
}

func (e *scenarioWorker) execute(ctx context.Context, req desktop.Request) (desktop.Response, error) {
	switch req.Operation {
	case "start":
		return e.prepare(ctx, req.Start)
	case "resume":
		return e.resume(ctx)
	case "observe":
		if !e.isResumed || e.host == nil {
			return desktop.Response{}, errors.New("client has not resumed")
		}
		observation, err := e.host.Observe(ctx, req.Milestone)
		observation.Artifacts = append(observation.Artifacts, e.host.EntryFrameArtifacts()...)
		if err != nil {
			return desktop.Response{Observation: observation}, fmt.Errorf("hostObserve: %w", err)
		}
		return desktop.Response{Observation: observation}, nil
	case "close":
		err := e.close(ctx)
		if err != nil {
			return desktop.Response{}, fmt.Errorf("hostClose: %w", err)
		}
		return desktop.Response{}, nil
	default:
		return desktop.Response{}, errors.New("unsupported worker operation")
	}
}

func (e *scenarioWorker) prepare(ctx context.Context, req *desktop.StartRequest) (desktop.Response, error) {
	if e.start != nil || req == nil {
		return desktop.Response{}, errors.New("worker start cannot be reused")
	}
	err := desktop.ValidateStart(*req)
	if err != nil {
		return desktop.Response{}, fmt.Errorf("workerPaths: %w", err)
	}
	e.start = req
	e.host, err = newScenarioHost(e.ctx, *req)
	if err != nil {
		return desktop.Response{}, fmt.Errorf("workerHost: %w", err)
	}
	clientReq, err := e.clientRequest()
	if err != nil {
		return desktop.Response{}, fmt.Errorf("workerClient: %w", err)
	}
	var fangCapability scenario.Capability
	e.client, fangCapability, err = startScenarioClient(e.ctx, ctx, clientReq)
	if err != nil {
		return desktop.Response{}, fmt.Errorf("workerSuspend: %w", err)
	}
	err = e.host.BindClientLiveness(e.client)
	if err != nil {
		return desktop.Response{}, fmt.Errorf("workerLiveness: %w", err)
	}
	err = e.host.BindEntryFrames(e.client)
	if err != nil {
		return desktop.Response{}, fmt.Errorf("workerEntry: %w", err)
	}
	e.capabilities = []scenario.Capability{scenarioCapability(), e.host.Capability(), fangCapability}
	capabilities := append([]scenario.Capability{scenario.NewCapability("runner", buildinfo.ID)}, e.capabilities...)
	err = scenario.ValidateCapabilities(capabilities)
	if err != nil {
		return desktop.Response{Capabilities: e.capabilities}, fmt.Errorf("workerCapabilities: %w", err)
	}
	err = e.provenance(ctx, clientReq.GamePath)
	if err != nil {
		return desktop.Response{Capabilities: e.capabilities}, fmt.Errorf("workerProvenance: %w", err)
	}
	return desktop.Response{Capabilities: e.capabilities}, nil
}

func (e *scenarioWorker) clientRequest() (scenarioClientRequest, error) {
	gamePath, workingDirectory, err := resolveGameExecutable(defaultGameExecutable(e.start.GameDirectory), e.start.GameDirectory)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientExecutable: %w", err)
	}
	err = desktop.CheckRegular(gamePath)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientBinary: %w", err)
	}
	err = validateGameData(workingDirectory)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientAssets: %w", err)
	}
	profileDirectory := filepath.Join(e.start.Paths.CacheDirectory, "client")
	err = os.Mkdir(profileDirectory, 0700)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientFresh: %w", err)
	}
	profile, err := configureClientEnvironment(profileDirectory)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientEnvironment: %w", err)
	}
	err = ensureWindowedClientPreference(profile)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientPreference: %w", err)
	}
	err = configureCinematicOptions(false, false)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientCinematic: %w", err)
	}
	err = configureBorderlessFullscreen(false)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientBorderless: %w", err)
	}
	err = configureLaunchJWT(e.host.JWT)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientCredential: %w", err)
	}
	err = configureClientTrace(filepath.Join(e.start.Paths.ReportDirectory, "client.jsonl"), e.start.Paths.ReportDirectory)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientTrace: %w", err)
	}
	err = configureSnapshotEnvironment("manual", filepath.Join(e.start.Paths.ReportDirectory, "snapshot-control.txt"))
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientSnapshot: %w", err)
	}
	// The registration format expects a hex launch identity independent of the run name.
	launchIdentity, randomErr := scenarioRandomText()
	if randomErr != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientIdentity: %w", randomErr)
	}
	err = configureClientFailure(filepath.Join(e.start.Paths.ReportDirectory, "client.failure.json"), launchIdentity[:32], e.start.Paths.ReportDirectory, "scenario", false)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientFailure: %w", err)
	}
	locales, err := detectClientLocales(e.start.GameDirectory)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientLocales: %w", err)
	}
	locale, err := resolveClientLocale("", locales)
	if err != nil {
		return scenarioClientRequest{}, fmt.Errorf("clientLocale: %w", err)
	}
	arguments := setClientLocaleArgument([]string{"-nolauncher", "-multipleInstances", "-userDataDir:" + profile}, locale)
	return scenarioClientRequest{RunID: e.start.RunID, GamePath: gamePath, WorkingDirectory: installDirectory(workingDirectory),
		FangPath: e.start.FangPath, Arguments: arguments, ServerAddress: e.host.Address}, nil
}

func (e *scenarioWorker) provenance(ctx context.Context, gamePath string) error {
	workerPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("versionExecutable: %w", err)
	}
	for _, component := range []string{"launcher", "server", "fang", "client"} {
		path := workerPath
		buildID := buildinfo.ID
		if component == "fang" {
			path = e.start.FangPath
		}
		if component == "client" {
			path = gamePath
			buildID = "shipped-client-sha256"
		}
		artifact, hashErr := desktop.HashArtifact(ctx, component+"_binary", path)
		if hashErr != nil {
			return fmt.Errorf("versionHash: %w", hashErr)
		}
		if component == "client" {
			buildID = "sha256:" + artifact.SHA256
		}
		e.versions = append(e.versions, scenario.Version{Component: component, BuildID: buildID, Artifact: artifact})
	}
	e.versions = append(e.versions, scenario.Version{Component: "content", BuildID: "sha256:" + e.host.ContentArtifact.SHA256, Artifact: e.host.ContentArtifact})
	return nil
}

func (e *scenarioWorker) resume(ctx context.Context) (desktop.Response, error) {
	if e.client == nil || e.isResumed {
		return desktop.Response{}, errors.New("client is not suspended for this run")
	}
	if e.host == nil || e.host.ProfileArtifact.Role != "profile_setup" ||
		e.host.ProfileArtifact.Path == "" || e.host.ProfileArtifact.SHA256 == "" ||
		filepath.Clean(filepath.Dir(e.host.ProfileArtifact.Path)) != filepath.Clean(e.start.Paths.ReportDirectory) {
		return desktop.Response{}, errors.New("fresh profile setup evidence is unavailable")
	}
	profileArtifact, err := desktop.HashArtifact(ctx, "profile_setup", e.host.ProfileArtifact.Path)
	if err != nil {
		return desktop.Response{}, fmt.Errorf("resumeProfile: %w", err)
	}
	if profileArtifact.SHA256 != e.host.ProfileArtifact.SHA256 {
		return desktop.Response{}, errors.New("fresh profile setup evidence digest changed")
	}
	if e.host.WebArtifact.Role != "web_setup" || e.host.WebArtifact.Path == "" || e.host.WebArtifact.SHA256 == "" ||
		filepath.Clean(filepath.Dir(e.host.WebArtifact.Path)) != filepath.Clean(e.start.Paths.ReportDirectory) {
		return desktop.Response{Observation: scenario.Observation{Outcome: scenario.Inconclusive,
			Artifacts: []scenario.Artifact{profileArtifact}}}, errors.New("fresh web setup evidence is unavailable")
	}
	webArtifact, err := desktop.HashArtifact(ctx, "web_setup", e.host.WebArtifact.Path)
	if err != nil {
		return desktop.Response{Observation: scenario.Observation{Outcome: scenario.Inconclusive,
			Artifacts: []scenario.Artifact{profileArtifact}}}, fmt.Errorf("resumeWeb: %w", err)
	}
	if webArtifact.SHA256 != e.host.WebArtifact.SHA256 {
		return desktop.Response{Observation: scenario.Observation{Outcome: scenario.Inconclusive,
			Artifacts: []scenario.Artifact{profileArtifact}}}, errors.New("fresh web setup evidence digest changed")
	}
	processID, err := e.client.Resume(ctx)
	if err != nil {
		return desktop.Response{Observation: scenario.Observation{Outcome: scenario.Inconclusive,
			Artifacts: []scenario.Artifact{profileArtifact, webArtifact}}}, fmt.Errorf("workerResume: %w", err)
	}
	e.isResumed = true
	artifact, err := writeScenarioWorkerEvidence(ctx, e.start.Paths.ReportDirectory, "launch.json", struct {
		RunID        string                `json:"run_id"`
		ProcessID    uint32                `json:"process_id"`
		ObservedAt   time.Time             `json:"observed_at"`
		Capabilities []scenario.Capability `json:"capabilities"`
	}{e.start.RunID, processID, time.Now().UTC(), e.capabilities})
	if err != nil {
		return desktop.Response{Observation: scenario.Observation{Outcome: scenario.Inconclusive,
			Artifacts: []scenario.Artifact{profileArtifact, webArtifact}}}, fmt.Errorf("resumeEvidence: %w", err)
	}
	return desktop.Response{Observation: scenario.Observation{Outcome: scenario.Passed,
		Detail: "owned client resumed after loaded Fang capability verification", Artifacts: []scenario.Artifact{artifact, profileArtifact, webArtifact}}, Versions: e.versions}, nil
}

func (e *scenarioWorker) close(ctx context.Context) error {
	errs := []error{}
	if e.client != nil {
		errs = append(errs, e.client.Close(ctx))
		e.client = nil
	}
	if e.host != nil {
		errs = append(errs, e.host.Close(ctx))
		e.host = nil
	}
	e.cancel()
	err := errors.Join(errs...)
	if err != nil {
		return fmt.Errorf("workerOwnedClose: %w", err)
	}
	return nil
}
