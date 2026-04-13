package handlers

import (
	"encoding/json"
	"os"
	"time"
)

// #region agent log
const agentDebugLogPath = "/Users/juanmanuelsolorzano/Developer/ez/local_siigo_fiscal/.cursor/debug-2b5b4b.log"

func agentDebugLog(hypothesisID, location, message string, data map[string]any) {
	payload := map[string]any{
		"sessionId":    "2b5b4b",
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	f, err := os.OpenFile(agentDebugLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

// #endregion
