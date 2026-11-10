package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// protocolVersion mirrors protocol/v1: the CLI is an ordinary protocol-v1
// client and must negotiate the same version as the desktop client.
const protocolVersion = 1

// maxMessageBytes mirrors the engine's JSONL frame limit.
const maxMessageBytes = 1024 * 1024

// RemoteError is a protocol-v1 failure returned by the engine.
type RemoteError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *RemoteError) Error() string {
	message := strings.TrimSpace(e.Message)
	if detail, ok := e.Details["message"].(string); ok {
		detail = strings.TrimSpace(detail)
		if detail != "" && detail != message {
			if message == "" {
				message = detail
			} else {
				message += ": " + detail
			}
		}
	}
	if e.Code == "" {
		return message
	}
	if message == "" {
		return e.Code
	}
	return e.Code + ": " + message
}

type envelope struct {
	Protocol int             `json:"protocol"`
	ID       string          `json:"id"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *RemoteError    `json:"error,omitempty"`
	Event    string          `json:"event,omitempty"`
	Sequence uint64          `json:"sequence,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// session is one protocol-v1 JSONL conversation with an engine host. The CLI
// is a one-shot client: it opens a session, issues one or a few calls, and
// exits, so a synchronous request/response loop is sufficient. Unsolicited
// events are surfaced on the diagnostics writer while a response is pending.
type session struct {
	conn    io.ReadWriteCloser
	scanner *bufio.Scanner
	ids     int
	events  io.Writer
}

func newSession(conn io.ReadWriteCloser, events io.Writer) *session {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), maxMessageBytes)
	return &session{conn: conn, scanner: scanner, events: events}
}

func (s *session) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// call sends one request and waits for the matching response. A nil result
// target discards the payload after error checking.
func (s *session) call(ctx context.Context, method string, params any, result any) error {
	if strings.TrimSpace(method) == "" {
		return errors.New("核心方法不能为空")
	}
	s.ids++
	id := fmt.Sprintf("cli-%d", s.ids)
	request := struct {
		Protocol int             `json:"protocol"`
		ID       string          `json:"id"`
		Method   string          `json:"method"`
		Params   json.RawMessage `json:"params,omitempty"`
	}{
		Protocol: protocolVersion,
		ID:       id,
		Method:   method,
	}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("序列化核心请求失败：%w", err)
		}
		request.Params = encoded
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("序列化核心请求失败：%w", err)
	}
	if len(payload) > maxMessageBytes-1 {
		return errors.New("核心请求超过 1 MiB 限制")
	}

	deadline := timectx(ctx)
	if deadline != nil {
		_ = s.setReadDeadline(*deadline)
		_ = s.setWriteDeadline(*deadline)
		defer s.setReadDeadline(time.Time{})
		defer s.setWriteDeadline(time.Time{})
	}
	if _, err := s.conn.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("发送核心请求失败：%w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待核心响应超时：%w", err)
		}
		if !s.scanner.Scan() {
			if scanErr := s.scanner.Err(); scanErr != nil {
				return fmt.Errorf("读取核心响应失败：%w", scanErr)
			}
			return errors.New("聚合核心连接已关闭")
		}
		line := bytes.TrimSpace(s.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message envelope
		if err := json.Unmarshal(line, &message); err != nil {
			continue
		}
		if message.Event != "" {
			s.reportEvent(&message)
			continue
		}
		if message.ID != id {
			continue
		}
		if message.Error != nil {
			return message.Error
		}
		if result == nil || len(message.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return fmt.Errorf("解析核心响应失败：%w", err)
		}
		return nil
	}
}

func (s *session) reportEvent(message *envelope) {
	if s.events == nil {
		return
	}
	data := strings.TrimSpace(string(message.Data))
	if data == "" || data == "null" {
		fmt.Fprintf(s.events, "[%s]\n", message.Event)
		return
	}
	fmt.Fprintf(s.events, "[%s] %s\n", message.Event, data)
}

// deadlineReader matches the overlapped pipe transports and os.Pipe-backed
// test transports; deadline-less streams simply skip deadline enforcement.
type deadlineReader interface {
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

func (s *session) setReadDeadline(deadline time.Time) error {
	if reader, ok := s.conn.(deadlineReader); ok {
		return reader.SetReadDeadline(deadline)
	}
	return nil
}

func (s *session) setWriteDeadline(deadline time.Time) error {
	if reader, ok := s.conn.(deadlineReader); ok {
		return reader.SetWriteDeadline(deadline)
	}
	return nil
}

func timectx(ctx context.Context) *time.Time {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	return &deadline
}
