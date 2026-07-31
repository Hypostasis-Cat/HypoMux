package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	supportLogMarker      = "=== HypoMux Acceleration Session |"
	supportLogMaxSessions = 3
	supportLogMaxBytes    = 5 * 1024 * 1024
	supportLogRateWindow  = 10 * time.Second
	supportLogTruncated   = "[日志维护] 较早内容已按日志大小上限裁剪。"
)

type SupportLogSession struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Mode      string    `json:"mode"`
	Adapters  []string  `json:"adapters"`
	Reason    string    `json:"reason,omitempty"`
	Bytes     int       `json:"bytes"`
	Text      string    `json:"text"`
}

type SupportLogSnapshot struct {
	Path        string              `json:"path"`
	MaxSessions int                 `json:"max_sessions"`
	MaxBytes    int                 `json:"max_bytes"`
	Sessions    []SupportLogSession `json:"sessions"`
}

type rateLimitState struct {
	Started    time.Time
	Suppressed int
	Label      string
}

type SupportLogStore struct {
	mu     sync.Mutex
	path   string
	active bool
	rates  map[string]rateLimitState
	now    func() time.Time
}

func NewSupportLogStore() *SupportLogStore {
	store := &SupportLogStore{
		path:  filepath.Join(settingsDirectory(), "logs", "app.log"),
		rates: map[string]rateLimitState{},
		now:   time.Now,
	}
	time.AfterFunc(2*time.Second, func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.active {
			return
		}
		store.removeLegacyRotationsLocked()
		store.enforceLimitLocked()
	})
	return store
}

func newSupportLogStore(path string) *SupportLogStore {
	store := &SupportLogStore{path: path, rates: map[string]rateLimitState{}, now: time.Now}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.removeLegacyRotationsLocked()
	store.enforceLimitLocked()
	return store
}

func (s *SupportLogStore) Start(mode string, adapters []string, context map[string]any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return false
	}
	sessions := s.readSessionsLocked()
	if len(sessions) >= supportLogMaxSessions {
		sessions = sessions[len(sessions)-(supportLogMaxSessions-1):]
	}
	s.rewriteLocked(sessions)
	s.rates = map[string]rateLimitState{}
	now := s.now().Local()
	id := fmt.Sprintf("%x", now.UnixNano())
	if len(id) > 12 {
		id = id[len(id)-12:]
	}
	names := make([]string, 0, len(adapters))
	for _, name := range adapters {
		if value := strings.TrimSpace(name); value != "" {
			names = append(names, sanitizeLogText(value))
		}
	}
	s.appendLocked(fmt.Sprintf(
		"%s id=%s | started=%s | mode=%s ===\nselected_adapters=%s",
		supportLogMarker, id, now.Format(time.RFC3339), sanitizeLogText(mode),
		strings.Join(names, ", "),
	))
	if len(context) > 0 {
		data, _ := json.Marshal(context)
		s.appendLocked("session_context=" + sanitizeLogText(string(data)))
	}
	s.active = true
	return true
}

func (s *SupportLogStore) RecordEvent(category string, event string, fields map[string]any) {
	payload := map[string]any{"category": category, "event": event}
	for key, value := range fields {
		if value != nil {
			payload[key] = value
		}
	}
	data, _ := json.Marshal(payload)
	s.Record("event="+string(data), true)
}

func (s *SupportLogStore) Record(message string, force bool) {
	text := sanitizeLogText(strings.TrimSpace(message))
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || (!force && !isKeySupportEvent(text)) {
		return
	}
	if !force && !s.allowRateLimitedLocked(text) {
		return
	}
	s.appendLocked(s.now().Local().Format(time.RFC3339) + " | " + text)
}

func (s *SupportLogStore) Finish(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	s.flushRateLimitsLocked()
	s.appendLocked(fmt.Sprintf(
		"=== HypoMux Acceleration Session End | ended=%s | reason=%s ===",
		s.now().Local().Format(time.RFC3339), sanitizeLogText(reason),
	))
	s.active = false
	s.enforceLimitLocked()
}

func (s *SupportLogStore) Snapshot() SupportLogSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := s.readSessionsLocked()
	sessions := make([]SupportLogSession, 0, len(raw))
	for _, text := range raw {
		sessions = append(sessions, parseSupportLogSession(text))
	}
	return SupportLogSnapshot{
		Path: s.path, MaxSessions: supportLogMaxSessions,
		MaxBytes: supportLogMaxBytes, Sessions: sessions,
	}
}

func (s *SupportLogStore) Raw() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return []byte{}, nil
	}
	return data, err
}

func (s *SupportLogStore) Directory() string {
	return filepath.Dir(s.path)
}

func (s *SupportLogStore) readSessionsLocked() []string {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	content := string(data)
	var sessions []string
	for {
		start := strings.Index(content, supportLogMarker)
		if start < 0 {
			break
		}
		content = content[start:]
		next := strings.Index(content[len(supportLogMarker):], supportLogMarker)
		if next < 0 {
			sessions = append(sessions, strings.TrimSpace(content))
			break
		}
		end := len(supportLogMarker) + next
		sessions = append(sessions, strings.TrimSpace(content[:end]))
		content = content[end:]
	}
	return sessions
}

func (s *SupportLogStore) rewriteLocked(sessions []string) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	content := strings.TrimSpace(strings.Join(sessions, "\n\n"))
	if content != "" {
		content += "\n"
	}
	temporary := s.path + ".tmp"
	if os.WriteFile(temporary, []byte(content), 0o600) == nil {
		_ = os.Rename(temporary, s.path)
	}
}

func (s *SupportLogStore) appendLocked(line string) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(strings.TrimRight(line, "\r\n") + "\n")
	_ = file.Close()
	s.enforceLimitLocked()
}

func (s *SupportLogStore) enforceLimitLocked() {
	data, err := os.ReadFile(s.path)
	if err != nil || len(data) <= supportLogMaxBytes {
		return
	}
	sessions := s.readSessionsLocked()
	var kept []string
	remaining := supportLogMaxBytes
	for index := len(sessions) - 1; index >= 0 && len(kept) < supportLogMaxSessions; index-- {
		session := sessions[index]
		size := len([]byte(session)) + 2
		if size <= remaining {
			kept = append([]string{session}, kept...)
			remaining -= size
			continue
		}
		if len(kept) == 0 {
			head, tailBudget := firstSessionLines(session), maxInt(0, remaining-256)
			tail := []byte(session)
			if len(tail) > tailBudget {
				tail = tail[len(tail)-tailBudget:]
				if newline := strings.IndexByte(string(tail), '\n'); newline >= 0 {
					tail = tail[newline+1:]
				}
			}
			trimmed := head + "\n" + supportLogTruncated + "\n" + string(tail)
			kept = []string{strings.TrimSpace(trimmed)}
		}
		break
	}
	s.rewriteLocked(kept)
}

func (s *SupportLogStore) removeLegacyRotationsLocked() {
	for _, suffix := range []string{".1", ".2", ".3"} {
		_ = os.Remove(s.path + suffix)
	}
}

func (s *SupportLogStore) allowRateLimitedLocked(text string) bool {
	key, label := supportRateKey(text)
	if key == "" {
		return true
	}
	now := s.now()
	state, exists := s.rates[key]
	if !exists {
		s.rates[key] = rateLimitState{Started: now, Label: label}
		return true
	}
	if now.Sub(state.Started) < supportLogRateWindow {
		state.Suppressed++
		s.rates[key] = state
		return false
	}
	s.appendRateSummaryLocked(state)
	s.rates[key] = rateLimitState{Started: now, Label: label}
	return true
}

func (s *SupportLogStore) flushRateLimitsLocked() {
	for _, state := range s.rates {
		s.appendRateSummaryLocked(state)
	}
	s.rates = map[string]rateLimitState{}
}

func (s *SupportLogStore) appendRateSummaryLocked(state rateLimitState) {
	if state.Suppressed <= 0 {
		return
	}
	s.appendLocked(fmt.Sprintf(
		"%s | [日志聚合] %s在 10 秒窗口内重复 %d 次，已省略。",
		s.now().Local().Format(time.RFC3339), state.Label, state.Suppressed,
	))
}

func supportRateKey(text string) (string, string) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "[socket-bind] ready"):
		return "socket-bind-ready", "socket-bind ready"
	case strings.Contains(text, "[连通失败]"):
		return "connect-failure", "出站连通失败"
	case strings.HasPrefix(lower, "[sing-box:stderr]") && strings.Contains(lower, "open connection"):
		return "sing-box-open", "sing-box 出站连接错误"
	default:
		return "", ""
	}
}

var (
	homePathPattern        = regexp.MustCompile(`(?i)[a-z]:\\users\\[^\\\s"]+`)
	escapedHomePathPattern = regexp.MustCompile(`(?i)[a-z]:\\\\users\\\\[^\\\s"]+`)
	secretPattern          = regexp.MustCompile(`(?i)(password|passwd|token|secret|authorization|cookie)(["']?\s*[:=]\s*["']?)[^"',\s}]+`)
)

func sanitizeLogText(value string) string {
	value = homePathPattern.ReplaceAllString(value, `%USERPROFILE%`)
	value = escapedHomePathPattern.ReplaceAllString(value, `%USERPROFILE%`)
	return secretPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
}

func isKeySupportEvent(value string) bool {
	lower := strings.ToLower(value)
	for _, word := range []string{
		"失败", "错误", "异常", "超时", "回滚", "启动", "停止", "验证",
		"[tun]", "[hypomux]", "dns", "bind", "route", "wintun", "sing-box",
		"error", "fail", "exception", "timeout", "fatal", "panic",
	} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func parseSupportLogSession(text string) SupportLogSession {
	session := SupportLogSession{Text: text, Bytes: len([]byte(text))}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return session
	}
	for _, field := range strings.Split(lines[0], "|") {
		field = strings.TrimSpace(strings.Trim(field, "="))
		switch {
		case strings.HasPrefix(field, "id="):
			session.ID = strings.TrimSpace(strings.TrimPrefix(field, "id="))
		case strings.HasPrefix(field, "started="):
			session.StartedAt, _ = time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(field, "started=")))
		case strings.HasPrefix(field, "mode="):
			session.Mode = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(field, "mode="), " ==="))
		}
	}
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "selected_adapters=") {
			for _, name := range strings.Split(strings.TrimPrefix(line, "selected_adapters="), ",") {
				if value := strings.TrimSpace(name); value != "" {
					session.Adapters = append(session.Adapters, value)
				}
			}
		}
		if strings.HasPrefix(line, "=== HypoMux Acceleration Session End") {
			for _, field := range strings.Split(line, "|") {
				field = strings.TrimSpace(strings.Trim(field, "="))
				if strings.HasPrefix(field, "ended=") {
					session.EndedAt, _ = time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(field, "ended=")))
				}
				if strings.HasPrefix(field, "reason=") {
					session.Reason = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(field, "reason="), " ==="))
				}
			}
		}
	}
	return session
}

func firstSessionLines(session string) string {
	lines := strings.Split(session, "\n")
	if len(lines) == 0 {
		return ""
	}
	head := []string{lines[0]}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "selected_adapters=") &&
			!strings.HasPrefix(line, "session_context=") {
			continue
		}
		if len(line) > 4096 {
			line = line[:4096] + "…"
		}
		head = append(head, line)
		if len(head) == 3 {
			break
		}
	}
	return strings.Join(head, "\n")
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
