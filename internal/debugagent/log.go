package debugagent

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

const sessionID = "fdb4b6"

func logPath() string {
	if p := strings.TrimSpace(os.Getenv("KRAN_AGENT_DEBUG_LOG")); p != "" {
		return p
	}
	return "/home/glaslos/workspace/kran/.cursor/debug-fdb4b6.log"
}

// Log appends one NDJSON line for debug sessions. Do not pass secrets or tokens in data.
func Log(hypothesisID, location, message string, data map[string]any) {
	type line struct {
		SessionID    string         `json:"sessionId"`
		HypothesisID string         `json:"hypothesisId"`
		Location     string         `json:"location"`
		Message      string         `json:"message"`
		Data         map[string]any `json:"data,omitempty"`
		Timestamp    int64          `json:"timestamp"`
		RunID        string         `json:"runId,omitempty"`
	}
	f, err := os.OpenFile(logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(line{
		SessionID:    sessionID,
		HypothesisID: hypothesisID,
		Location:     location,
		Message:      message,
		Data:         data,
		Timestamp:    time.Now().UnixMilli(),
	})
}
