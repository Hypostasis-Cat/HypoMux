package engineclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ProtocolVersion = 1
	MaxMessageBytes = 1024 * 1024
)

type RemoteError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *RemoteError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type response struct {
	Protocol int             `json:"protocol"`
	ID       string          `json:"id"`
	Result   json.RawMessage `json:"result"`
	Error    *RemoteError    `json:"error,omitempty"`
	Event    string          `json:"event,omitempty"`
	Sequence uint64          `json:"sequence,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

type Event struct {
	Name     string
	Sequence uint64
	Data     json.RawMessage
}

type Client struct {
	mu               sync.Mutex
	startGate        chan struct{}
	writeGate        chan struct{}
	session          *coreSession
	pending          map[string]chan response
	sequence         atomic.Uint64
	path             string
	hello            Hello
	normalLauncher   coreLauncher
	elevatedLauncher coreLauncher
	events           chan Event
}

type Hello struct {
	Engine          string              `json:"engine"`
	EngineVersion   string              `json:"engine_version"`
	Commit          string              `json:"commit"`
	ProtocolVersion int                 `json:"protocol_version"`
	Capabilities    []string            `json:"capabilities"`
	Modes           []string            `json:"modes"`
	ModeFeatures    map[string][]string `json:"mode_features"`
	Elevated        bool                `json:"elevated"`
	PID             int                 `json:"pid"`
}

func New() *Client {
	return newClient(stdioLauncher{}, newPrivilegedLauncher())
}

func newClient(normal, elevated coreLauncher) *Client {
	return &Client{
		pending:          map[string]chan response{},
		startGate:        make(chan struct{}, 1),
		writeGate:        make(chan struct{}, 1),
		normalLauncher:   normal,
		elevatedLauncher: elevated,
		events:           make(chan Event, 64),
	}
}

func (c *Client) Events() <-chan Event {
	return c.events
}

func (c *Client) ExecutablePath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path
}

func (c *Client) Hello() Hello {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hello
}

func (c *Client) Ensure(ctx context.Context) (Hello, error) {
	return c.ensure(ctx, false)
}

// EnsureElevated starts or reuses an authenticated elevated Core. The desktop
// UI process itself is never elevated.
func (c *Client) EnsureElevated(ctx context.Context) (Hello, error) {
	return c.ensure(ctx, true)
}

func (c *Client) ensure(ctx context.Context, requireElevated bool) (Hello, error) {
	select {
	case c.startGate <- struct{}{}:
		defer func() { <-c.startGate }()
	case <-ctx.Done():
		return Hello{}, fmt.Errorf("等待聚合核心启动事务超时：%w", ctx.Err())
	}

	c.mu.Lock()
	active := c.session != nil
	hello := c.hello
	c.mu.Unlock()
	if active && hello.ProtocolVersion == ProtocolVersion && (!requireElevated || hello.Elevated) {
		return hello, nil
	}
	if active {
		c.killCurrent(errors.New("正在切换聚合核心权限级别"))
	}

	path, err := ResolveExecutable()
	if err != nil {
		return Hello{}, err
	}
	launcher := c.normalLauncher
	if requireElevated {
		launcher = c.elevatedLauncher
	}
	session, err := launcher.Launch(ctx, path)
	if err != nil {
		return Hello{}, err
	}

	c.mu.Lock()
	if c.session != nil {
		c.mu.Unlock()
		_ = session.closeTransport()
		_ = session.process.Kill()
		return Hello{}, errors.New("聚合核心已由另一启动请求连接")
	}
	c.session = session
	c.path = path
	c.pending = map[string]chan response{}
	c.hello = Hello{}
	c.mu.Unlock()

	go c.readLoop(session)
	go func() {
		_ = session.process.Wait()
		c.failSession(session, errors.New("聚合核心进程已退出"))
	}()

	var negotiated Hello
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Request(requestCtx, "engine.hello", nil, &negotiated); err != nil {
		c.killCurrent(fmt.Errorf("聚合核心协议协商失败：%w", err))
		return Hello{}, fmt.Errorf("聚合核心协议协商失败：%w", err)
	}
	if negotiated.ProtocolVersion != ProtocolVersion {
		c.killCurrent(errors.New("聚合核心协议版本不兼容"))
		return Hello{}, fmt.Errorf("不兼容的聚合核心协议版本：%d", negotiated.ProtocolVersion)
	}
	if requireElevated && !negotiated.Elevated {
		c.killCurrent(errors.New("独立聚合核心未获得管理员权限"))
		return Hello{}, errors.New("独立聚合核心未获得管理员权限；TUN 启动已取消")
	}
	c.mu.Lock()
	if c.session == session {
		c.hello = negotiated
	}
	c.mu.Unlock()
	return negotiated, nil
}

func (c *Client) Request(ctx context.Context, method string, params any, target any) error {
	if strings.TrimSpace(method) == "" {
		return errors.New("核心方法不能为空")
	}
	id := fmt.Sprintf("wails-%d", c.sequence.Add(1))
	request := map[string]any{
		"protocol": ProtocolVersion,
		"id":       id,
		"method":   method,
	}
	if params != nil {
		request["params"] = params
	}
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("序列化核心请求失败：%w", err)
	}
	if len(data) > MaxMessageBytes {
		return errors.New("核心请求超过 1 MiB 限制")
	}
	reply := make(chan response, 1)
	c.mu.Lock()
	if c.session == nil || c.session.writer == nil {
		c.mu.Unlock()
		return errors.New("聚合核心未连接")
	}
	writer := c.session.writer
	c.pending[id] = reply
	c.mu.Unlock()

	select {
	case c.writeGate <- struct{}{}:
	case <-ctx.Done():
		c.removePending(id)
		return fmt.Errorf("等待发送核心请求超时：%w", ctx.Err())
	}
	writeResult := make(chan error, 1)
	go func() {
		_, writeErr := writer.Write(append(data, '\n'))
		writeResult <- writeErr
	}()
	var writeErr error
	select {
	case writeErr = <-writeResult:
		<-c.writeGate
	case <-ctx.Done():
		// Closing the transport interrupts a blocked named-pipe/stdio write and
		// prevents one wedged Core session from freezing every future request.
		c.killCurrent(fmt.Errorf("发送核心请求超时：%w", ctx.Err()))
		// The old writer belongs to the killed session. Releasing the gate lets
		// a newly negotiated session recover even if a broken OS transport never
		// wakes the abandoned write goroutine.
		<-c.writeGate
		return fmt.Errorf("发送核心请求超时：%w", ctx.Err())
	}
	if writeErr != nil {
		c.removePending(id)
		return fmt.Errorf("发送核心请求失败：%w", writeErr)
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return fmt.Errorf("等待核心响应超时：%w", ctx.Err())
	case result := <-reply:
		if result.Error != nil {
			return result.Error
		}
		if target == nil || len(result.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(result.Result, target); err != nil {
			return fmt.Errorf("解析核心响应失败：%w", err)
		}
		return nil
	}
}

func (c *Client) Shutdown(ctx context.Context) {
	var ignored map[string]any
	_ = c.Request(ctx, "host.shutdown", nil, &ignored)
	c.Kill()
}

func (c *Client) Kill() {
	c.killCurrent(errors.New("聚合核心连接已关闭"))
}

func (c *Client) killCurrent(reason error) {
	c.mu.Lock()
	session := c.session
	c.session = nil
	c.path = ""
	c.hello = Hello{}
	pending := c.pending
	c.pending = map[string]chan response{}
	c.mu.Unlock()
	if session != nil {
		_ = session.closeTransport()
		_ = session.process.Kill()
	}
	failReplies(pending, reason)
}

func (c *Client) readLoop(session *coreSession) {
	scanner := bufio.NewScanner(session.reader)
	scanner.Buffer(make([]byte, 64*1024), MaxMessageBytes)
	for scanner.Scan() {
		var message response
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if message.Event != "" {
			select {
			case c.events <- Event{
				Name: message.Event, Sequence: message.Sequence,
				Data: append(json.RawMessage(nil), message.Data...),
			}:
			default:
			}
			continue
		}
		if message.ID == "" {
			continue
		}
		c.mu.Lock()
		if c.session != session {
			c.mu.Unlock()
			return
		}
		reply := c.pending[message.ID]
		delete(c.pending, message.ID)
		c.mu.Unlock()
		if reply != nil {
			reply <- message
		}
	}
	c.failSession(session, errors.New("聚合核心输出已关闭"))
}

func (c *Client) failSession(session *coreSession, reason error) {
	c.mu.Lock()
	if c.session != session {
		c.mu.Unlock()
		return
	}
	c.session = nil
	c.path = ""
	c.hello = Hello{}
	pending := c.pending
	c.pending = map[string]chan response{}
	c.mu.Unlock()
	_ = session.closeTransport()
	failReplies(pending, reason)
}

func (c *Client) removePending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func failReplies(pending map[string]chan response, reason error) {
	for _, reply := range pending {
		reply <- response{Error: &RemoteError{Code: "disconnected", Message: reason.Error()}}
	}
}

func (s *coreSession) closeTransport() error {
	if s == nil || s.close == nil {
		return nil
	}
	s.closeMu.Do(func() {
		s.closeErr = s.close()
	})
	return s.closeErr
}

func ResolveExecutable() (string, error) {
	candidates := make([]string, 0, 16)
	if configured := os.Getenv("HYPOMUX_ENGINE_PATH"); configured != "" {
		candidates = append(candidates, os.ExpandEnv(configured))
	}
	if executable, err := os.Executable(); err == nil {
		root := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(root, "bin", "hypomux-engine.exe"),
			filepath.Join(root, "hypomux-engine.exe"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		for current, count := cwd, 0; count < 6; count++ {
			candidates = append(candidates,
				filepath.Join(current, "hypomux-engine.exe"),
				filepath.Join(current, "bin", "hypomux-engine.exe"),
				filepath.Join(current, "dist", "hypomux-engine.exe"),
			)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		key := strings.ToLower(absolute)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
			return absolute, nil
		}
	}
	return "", errors.New("未找到 hypomux-engine.exe；可设置 HYPOMUX_ENGINE_PATH")
}
