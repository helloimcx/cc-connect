package opencode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

// TestOpencodeSessionEntry_Unmarshal verifies that OpenCode's
// `session list --format json` output can be correctly parsed.
//
// OpenCode returns `updated` and `created` as Unix timestamps in
// milliseconds (int64), not strings. This test prevents regression
// of the unmarshal error:
//
//	json: cannot unmarshal number into Go struct field opencodeSessionEntry.updated of type string
func TestOpencodeSessionEntry_Unmarshal(t *testing.T) {
	jsonData := `[
  {
    "id": "ses_2eb11bb11ffeYwQZOj25mlmGMc",
    "title": "Test Session",
    "updated": 1774174646445,
    "created": 1774172652782,
    "projectId": "b80385ead03e8b450bdb2016d434aad318f93c16",
    "directory": "/path/to/project"
  }
]`

	var entries []opencodeSessionEntry
	if err := json.Unmarshal([]byte(jsonData), &entries); err != nil {
		t.Fatalf("Failed to unmarshal OpenCode session list: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.ID != "ses_2eb11bb11ffeYwQZOj25mlmGMc" {
		t.Errorf("ID = %q, want %q", e.ID, "ses_2eb11bb11ffeYwQZOj25mlmGMc")
	}
	if e.Title != "Test Session" {
		t.Errorf("Title = %q, want %q", e.Title, "Test Session")
	}
	if e.Updated != 1774174646445 {
		t.Errorf("Updated = %d, want %d", e.Updated, 1774174646445)
	}
	if e.Created != 1774172652782 {
		t.Errorf("Created = %d, want %d", e.Created, 1774172652782)
	}
}

// TestNewOpencodeSession_ContinueSessionTreatedAsFresh verifies that
// the ContinueSession sentinel (__continue__) is not passed as a literal
// session ID to the CLI. This was fixed in PR #249.
func TestNewOpencodeSession_ContinueSessionTreatedAsFresh(t *testing.T) {
	s, err := newOpencodeSession(context.Background(), "echo", "/tmp", "", "default", core.ContinueSession, nil)
	if err != nil {
		t.Fatalf("newOpencodeSession: %v", err)
	}
	defer s.Close()

	if got := s.CurrentSessionID(); got != "" {
		t.Errorf("ContinueSession should be treated as fresh: chatID = %q, want empty", got)
	}
}

func TestHandleToolUse_ErrorEmitsFailedToolResult(t *testing.T) {
	s, err := newOpencodeSession(context.Background(), "echo", "/tmp", "", "default", "", nil)
	if err != nil {
		t.Fatalf("newOpencodeSession: %v", err)
	}
	defer s.Close()

	s.handleEvent(map[string]any{
		"type": "tool_use",
		"part": map[string]any{
			"type": "tool",
			"tool": "bash",
			"state": map[string]any{
				"status": "error",
				"title":  "Delete temp file",
				"error":  "The user rejected permission to use this specific tool call.",
			},
		},
	})

	useEvt := <-s.Events()
	if useEvt.Type != core.EventToolUse {
		t.Fatalf("first event type = %v, want %v", useEvt.Type, core.EventToolUse)
	}
	if useEvt.ToolName != "bash" {
		t.Fatalf("tool name = %q, want bash", useEvt.ToolName)
	}
	if useEvt.ToolInput != "Delete temp file" {
		t.Fatalf("tool input = %q, want %q", useEvt.ToolInput, "Delete temp file")
	}

	resultEvt := <-s.Events()
	if resultEvt.Type != core.EventToolResult {
		t.Fatalf("second event type = %v, want %v", resultEvt.Type, core.EventToolResult)
	}
	if resultEvt.ToolStatus != "failed" {
		t.Fatalf("tool status = %q, want failed", resultEvt.ToolStatus)
	}
	if resultEvt.ToolSuccess == nil || *resultEvt.ToolSuccess {
		t.Fatalf("tool success = %#v, want false", resultEvt.ToolSuccess)
	}
	if resultEvt.ToolResult != "The user rejected permission to use this specific tool call." {
		t.Fatalf("tool result = %q", resultEvt.ToolResult)
	}
}

// verify Agent implements core.Agent
var _ core.Agent = (*Agent)(nil)
