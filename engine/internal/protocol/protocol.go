package protocol

import "encoding/json"

const Version = 1

// Request is one newline-delimited JSON command sent by a UI client.
type Request struct {
	Protocol int             `json:"protocol"`
	ID       string          `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
}

// Error is stable across transports and UI implementations.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Response contains exactly one of Result or Error.
type Response struct {
	Protocol int    `json:"protocol"`
	ID       string `json:"id"`
	Result   any    `json:"result,omitempty"`
	Error    *Error `json:"error,omitempty"`
}

// Event is an unsolicited engine notification.
type Event struct {
	Protocol int    `json:"protocol"`
	Sequence uint64 `json:"sequence"`
	Event    string `json:"event"`
	Data     any    `json:"data,omitempty"`
}

func Result(id string, result any) Response {
	return Response{
		Protocol: Version,
		ID:       id,
		Result:   result,
	}
}

func Failure(id, code, message string, details map[string]any) Response {
	return Response{
		Protocol: Version,
		ID:       id,
		Error: &Error{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
}

func Notification(sequence uint64, name string, data any) Event {
	return Event{
		Protocol: Version,
		Sequence: sequence,
		Event:    name,
		Data:     data,
	}
}
