package tun

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultStartupTimeout = 20 * time.Second
	defaultReadyStableFor = 750 * time.Millisecond
	configCheckTimeout    = 10 * time.Second
	cleanupTimeout        = 15 * time.Second
	maxLogLineBytes       = 64 * 1024
	tunInterfaceName      = "HypoMux-Tun"
	tunIPv4Address        = "172.19.0.1"
)

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

type Config struct {
	Executable     string
	ConfigPath     string
	StartupTimeout time.Duration
}

type Status struct {
	State      State      `json:"state"`
	Generation uint64     `json:"generation,omitempty"`
	PID        int        `json:"pid,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	ExitedAt   *time.Time `json:"exited_at,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	ConfigPath string     `json:"config_path,omitempty"`
}

type commandFactory func(
	context.Context,
	string,
	...string,
) *exec.Cmd

type processContainment interface {
	Close() error
}

type sidecarRun struct {
	command     *exec.Cmd
	done        chan struct{}
	intentional atomic.Bool
	containment processContainment
	generation  uint64
	configPath  string
	startedAt   time.Time
	stderrMu    sync.Mutex
	lastStderr  string

	cleanupOnce sync.Once
	cleanupDone chan struct{}
	cleanupErr  error
}

type Supervisor struct {
	mu     sync.Mutex
	stopMu sync.Mutex
	status Status
	run    *sidecarRun

	command        commandFactory
	cleanup        func(context.Context) error
	contain        func(*os.Process) (processContainment, error)
	configure      func(*exec.Cmd)
	onLog          func(string)
	onUnexpected   func(Status)
	startupReady   func() bool
	readyStableFor time.Duration
	nextGeneration uint64
}

func NewSupervisor() *Supervisor {
	return &Supervisor{
		status:         Status{State: StateStopped},
		command:        exec.CommandContext,
		cleanup:        cleanupPlatform,
		contain:        containProcess,
		configure:      configureProcess,
		startupReady:   tunPlatformReady,
		readyStableFor: defaultReadyStableFor,
	}
}

func (s *Supervisor) SetHandlers(
	onLog func(string),
	onUnexpected func(Status),
) {
	s.mu.Lock()
	s.onLog = onLog
	s.onUnexpected = onUnexpected
	s.mu.Unlock()
}

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Supervisor) Activate(ctx context.Context, config Config) (Status, error) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()

	normalized, err := normalizeConfig(config)
	if err != nil {
		return s.Status(), err
	}
	s.mu.Lock()
	if s.status.State != StateStopped && s.status.State != StateFailed {
		state := s.status.State
		s.mu.Unlock()
		return s.Status(), fmt.Errorf("TUN sidecar cannot start from %s", state)
	}
	s.nextGeneration++
	generation := s.nextGeneration
	s.status = Status{
		State:      StateStarting,
		Generation: generation,
		ConfigPath: normalized.ConfigPath,
	}
	s.mu.Unlock()

	if err := s.validateConfig(ctx, normalized); err != nil {
		s.failStart(err)
		return s.Status(), err
	}
	s.emitLog("[TUN] sing-box configuration check passed")
	if err := s.cleanupWithTimeout(ctx); err != nil {
		err = fmt.Errorf("clean stale HypoMux TUN state: %w", err)
		s.failStart(err)
		return s.Status(), err
	}

	command := s.command(
		context.Background(),
		normalized.Executable,
		"run",
		"-c",
		normalized.ConfigPath,
	)
	command.Dir = filepath.Dir(normalized.Executable)
	s.configure(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		s.failStart(err)
		return s.Status(), fmt.Errorf("open sing-box stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		s.failStart(err)
		return s.Status(), fmt.Errorf("open sing-box stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		s.failStart(err)
		return s.Status(), fmt.Errorf("start sing-box: %w", err)
	}
	containment, err := s.contain(command.Process)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		s.failStart(err)
		return s.Status(), fmt.Errorf("contain sing-box process: %w", err)
	}

	startedAt := time.Now().UTC()
	run := &sidecarRun{
		command:     command,
		done:        make(chan struct{}),
		containment: containment,
		cleanupDone: make(chan struct{}),
		generation:  generation,
		configPath:  normalized.ConfigPath,
		startedAt:   startedAt,
	}
	s.mu.Lock()
	s.run = run
	s.status.PID = command.Process.Pid
	s.status.StartedAt = timePointer(startedAt)
	s.mu.Unlock()
	s.emitLog(fmt.Sprintf(
		"[TUN] sing-box process started (PID=%d), waiting for stable takeover",
		command.Process.Pid,
	))

	go s.pump(run, stdout, "stdout")
	go s.pump(run, stderr, "stderr")
	go s.waitProcess(run)

	timer := time.NewTimer(normalized.StartupTimeout)
	defer timer.Stop()
	readyTicker := time.NewTicker(40 * time.Millisecond)
	defer readyTicker.Stop()
	var readySince time.Time
	for {
		select {
		case <-timer.C:
			err := fmt.Errorf(
				"TUN interface %s did not become ready within %s",
				tunInterfaceName, normalized.StartupTimeout,
			)
			return s.failStartedRun(run, err)
		case <-readyTicker.C:
			if s.startupReady != nil && s.startupReady() {
				if readySince.IsZero() {
					readySince = time.Now()
				}
				if time.Since(readySince) >= s.readyStableFor {
					return s.markRunning(run)
				}
			} else {
				readySince = time.Time{}
			}
		case <-run.done:
			status := s.Status()
			if status.LastError == "" {
				status.LastError = "sing-box exited during startup"
			}
			return status, errors.New(status.LastError)
		case <-ctx.Done():
			err := fmt.Errorf("activate TUN sidecar: %w", ctx.Err())
			return s.failStartedRun(run, err)
		}
	}
}

func (s *Supervisor) failStartedRun(run *sidecarRun, cause error) (Status, error) {
	run.intentional.Store(true)
	if detail := strings.TrimSpace(run.stderrSnapshot()); detail != "" &&
		!strings.Contains(cause.Error(), detail) {
		cause = fmt.Errorf("%w; last sing-box error: %s", cause, detail)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout+5*time.Second)
	stopErr := s.terminateRun(run, stopCtx)
	cancel()
	if stopErr != nil {
		cause = errors.Join(cause, fmt.Errorf("rollback failed TUN startup: %w", stopErr))
	}
	s.mu.Lock()
	if s.run == run {
		status := s.status
		status.State = StateFailed
		status.PID = 0
		status.LastError = cause.Error()
		if status.ExitedAt == nil {
			now := time.Now().UTC()
			status.ExitedAt = &now
		}
		s.status = status
		s.run = nil
	}
	status := s.status
	s.mu.Unlock()
	return status, cause
}

func (s *Supervisor) markRunning(run *sidecarRun) (Status, error) {
	s.mu.Lock()
	if s.run != run || s.status.State != StateStarting {
		status := s.status
		s.mu.Unlock()
		return status, errors.New("sing-box exited during startup")
	}
	s.status.State = StateRunning
	status := s.status
	s.mu.Unlock()
	s.emitLog("[TUN] sing-box is stable and owns TUN/WFP/routes")
	return status, nil
}

func tunInterfaceWithExpectedAddress() (*net.Interface, bool) {
	device, err := net.InterfaceByName(tunInterfaceName)
	if err != nil || device.Flags&net.FlagUp == 0 {
		return nil, false
	}
	addresses, err := device.Addrs()
	if err != nil {
		return nil, false
	}
	for _, address := range addresses {
		value := address.String()
		host, _, splitErr := net.ParseCIDR(value)
		if splitErr == nil && host.String() == tunIPv4Address {
			return device, true
		}
	}
	return nil, false
}

func (s *Supervisor) Stop(ctx context.Context) (Status, error) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()

	s.mu.Lock()
	run := s.run
	if run == nil {
		accepted := s.status.State != StateStopped
		s.status = Status{State: StateStopped}
		status := s.status
		s.mu.Unlock()
		if accepted {
			return status, nil
		}
		return status, nil
	}
	run.intentional.Store(true)
	s.status.State = StateStopping
	s.mu.Unlock()

	err := s.terminateRun(run, ctx)
	s.mu.Lock()
	if s.run == run {
		s.run = nil
	}
	s.status = Status{State: StateStopped}
	status := s.status
	s.mu.Unlock()
	return status, err
}

func (s *Supervisor) validateConfig(ctx context.Context, config Config) error {
	checkCtx, cancel := context.WithTimeout(ctx, configCheckTimeout)
	defer cancel()
	command := s.command(
		checkCtx,
		config.Executable,
		"check",
		"--disable-color",
		"-c",
		config.ConfigPath,
	)
	command.Dir = filepath.Dir(config.Executable)
	s.configure(command)
	output := &tailBuffer{limit: 32 * 1024}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(output.String())
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("sing-box configuration check failed: %s", detail)
}

func (s *Supervisor) waitProcess(run *sidecarRun) {
	err := run.command.Wait()
	exitedAt := time.Now().UTC()
	exitCode := -1
	if run.command.ProcessState != nil {
		exitCode = run.command.ProcessState.ExitCode()
	}
	unexpected := !run.intentional.Load()
	status := Status{
		State:      StateStopped,
		Generation: run.generation,
		PID:        0,
		StartedAt:  timePointer(run.startedAt),
		ExitedAt:   timePointer(exitedAt),
		ExitCode:   intPointer(exitCode),
		ConfigPath: run.configPath,
	}
	if unexpected {
		status.State = StateFailed
		status.LastError = exitError(exitCode, err, run.stderrSnapshot())
	}

	s.mu.Lock()
	if s.run == run {
		s.status = status
	}
	s.mu.Unlock()

	cleanupErr := s.cleanupRun(run, context.Background())
	if cleanupErr != nil {
		status.LastError = errors.Join(
			errors.New(status.LastError),
			fmt.Errorf("TUN cleanup: %w", cleanupErr),
		).Error()
		s.mu.Lock()
		if s.run == run && unexpected {
			s.status = status
		}
		s.mu.Unlock()
	}
	// A completed run includes its network cleanup. Stop and Activate callers
	// must never observe a closed done channel while owned routes or the
	// Wintun device are still being removed.
	close(run.done)
	if unexpected && !run.intentional.Load() {
		s.mu.Lock()
		handler := s.onUnexpected
		s.mu.Unlock()
		if handler != nil {
			handler(status)
		}
	}
}

func (r *sidecarRun) stderrSnapshot() string {
	r.stderrMu.Lock()
	defer r.stderrMu.Unlock()
	return r.lastStderr
}

func (s *Supervisor) terminateRun(
	run *sidecarRun,
	ctx context.Context,
) error {
	if run.containment != nil {
		_ = run.containment.Close()
	}
	if run.command.Process != nil {
		_ = run.command.Process.Kill()
	}
	select {
	case <-run.done:
	case <-ctx.Done():
		return fmt.Errorf("wait for sing-box stop: %w", ctx.Err())
	}
	if err := s.cleanupRun(run, ctx); err != nil {
		return fmt.Errorf("clean TUN state: %w", err)
	}
	return nil
}

func (s *Supervisor) cleanupRun(
	run *sidecarRun,
	ctx context.Context,
) error {
	run.cleanupOnce.Do(func() {
		defer close(run.cleanupDone)
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			cleanupTimeout,
		)
		run.cleanupErr = s.cleanup(cleanupCtx)
		cancel()
		if run.containment != nil {
			run.cleanupErr = errors.Join(
				run.cleanupErr,
				run.containment.Close(),
			)
		}
	})
	select {
	case <-run.cleanupDone:
		return run.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) cleanupWithTimeout(ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()
	return s.cleanup(cleanupCtx)
}

func (s *Supervisor) pump(run *sidecarRun, stream io.Reader, name string) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 4096), maxLogLineBytes)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if name == "stderr" {
			run.stderrMu.Lock()
			run.lastStderr = text
			run.stderrMu.Unlock()
		}
		s.emitLog(fmt.Sprintf("[sing-box:%s] %s", name, text))
	}
	if err := scanner.Err(); err != nil {
		s.emitLog(fmt.Sprintf("[TUN] read sing-box %s: %v", name, err))
		_, _ = io.Copy(io.Discard, stream)
	}
}

func (s *Supervisor) failStart(err error) {
	now := time.Now().UTC()
	s.mu.Lock()
	s.status.State = StateFailed
	s.status.ExitedAt = timePointer(now)
	s.status.LastError = err.Error()
	s.mu.Unlock()
}

func exitError(exitCode int, waitErr error, stderr string) string {
	result := fmt.Sprintf("sing-box exited unexpectedly (code=%d)", exitCode)
	if stderr != "" {
		result += ": " + stderr
	} else if waitErr != nil {
		result += ": " + waitErr.Error()
	}
	return result
}

func (s *Supervisor) emitLog(message string) {
	s.mu.Lock()
	handler := s.onLog
	s.mu.Unlock()
	if handler != nil {
		handler(message)
	}
}

func normalizeConfig(config Config) (Config, error) {
	executable, err := filepath.Abs(strings.TrimSpace(config.Executable))
	if err != nil {
		return Config{}, fmt.Errorf("resolve sing-box executable: %w", err)
	}
	configPath, err := filepath.Abs(strings.TrimSpace(config.ConfigPath))
	if err != nil {
		return Config{}, fmt.Errorf("resolve sing-box config: %w", err)
	}
	if err := requireRegularFile(executable, "sing-box executable"); err != nil {
		return Config{}, err
	}
	if err := requireRegularFile(configPath, "sing-box config"); err != nil {
		return Config{}, err
	}
	timeout := config.StartupTimeout
	if timeout <= 0 {
		timeout = defaultStartupTimeout
	}
	if timeout < 100*time.Millisecond || timeout > 60*time.Second {
		return Config{}, errors.New("startup timeout must be between 100ms and 60s")
	}
	return Config{
		Executable:     filepath.Clean(executable),
		ConfigPath:     filepath.Clean(configPath),
		StartupTimeout: timeout,
	}, nil
}

func requireRegularFile(path string, label string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s is required", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s is unavailable: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", label)
	}
	return nil
}

func timePointer(value time.Time) *time.Time {
	result := value
	return &result
}

func intPointer(value int) *int {
	result := value
	return &result
}

type tailBuffer struct {
	data  []byte
	limit int
}

func (b *tailBuffer) Write(payload []byte) (int, error) {
	if b.limit <= 0 {
		return len(payload), nil
	}
	b.data = append(b.data, payload...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(payload), nil
}

func (b *tailBuffer) String() string {
	return string(b.data)
}
