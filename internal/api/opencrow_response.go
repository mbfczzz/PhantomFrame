package api

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

var openCrowTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)

func openCrowResponseID(row Row) string {
	return inventoryID(first(row["id"], row["task_id"], row["taskId"]))
}

// OpenCrow can return a durable task receipt with a transient HTTP error.
// Preserve only a complete, unambiguous receipt; never retry the POST itself.
func openCrowAcceptedErrorResponse(raw []byte, model string) (any, bool) {
	if len(raw) > 1024*1024 {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var row Row
	if decoder.Decode(&row) != nil || row == nil {
		return nil, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, false
	}
	if !openCrowTaskIDPattern.MatchString(openCrowResponseID(row)) || inventoryEnvelopeError("opencrow", row) {
		return nil, false
	}
	if returned := str(row["model"]); returned != "" && returned != model {
		return nil, false
	}
	switch inventoryState("opencrow", row) {
	case "pending", "submitted", "processing", "queued", "running", "succeeded", "failed", "timeout", "canceled", "cancelled":
		return row, true
	}
	return nil, false
}

func openCrowPollValue(taskID string, root Row) Row {
	// The gateway's top-level ID is authoritative. Nested IDs may belong to its
	// upstream and need not equal the public query ID.
	if returned := openCrowResponseID(root); returned != "" {
		if returned != taskID {
			return Row{"status": "processing"}
		}
		return root
	}
	if nested, ok := root["data"].(map[string]any); ok {
		if returned := openCrowResponseID(nested); returned != "" && returned != taskID {
			return Row{"status": "processing"}
		}
		return root
	}
	items := array(root["data"])
	matched := []any{}
	identified := false
	for _, raw := range items {
		item := object(raw)
		if returned := openCrowResponseID(item); returned != "" {
			identified = true
			if returned == taskID {
				matched = append(matched, item)
			}
		}
	}
	if identified {
		if len(matched) == 0 {
			return Row{"status": "processing"}
		}
		root = clone(root)
		root["data"] = matched
		if str(root["status"]) == "" {
			root["status"] = object(matched[0])["status"]
		}
	}
	return root
}
