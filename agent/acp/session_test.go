package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/gorilla/websocket"
)

func TestACPStdioSessionHandshakeAndSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := newACPSession(
		ctx,
		os.Args[0],
		[]string{"-test.run=TestACPHelperProcess", "--", "stdio"},
		[]string{"GO_WANT_HELPER_PROCESS=1"},
		t.TempDir(),
		"",
		"",
	)
	if err != nil {
		t.Fatalf("newACPSession: %v", err)
	}
	defer func() { _ = s.Close() }()

	if got := s.CurrentSessionID(); got != "stdio-session" {
		t.Fatalf("CurrentSessionID = %q, want stdio-session", got)
	}

	if err := s.Send("hello", nil, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	text := waitForEvent(t, s.Events(), func(ev core.Event) bool {
		return ev.Type == core.EventText && ev.Content == "hello from stdio"
	})
	if text.SessionID != "stdio-session" {
		t.Fatalf("text session id = %q", text.SessionID)
	}

	waitForEvent(t, s.Events(), func(ev core.Event) bool {
		return ev.Type == core.EventResult && ev.SessionID == "stdio-session"
	})
}

func TestACPWSSessionHandshakeAndSend(t *testing.T) {
	wsURL := startMockACPWSServer(t, func(conn *websocket.Conn, msg map[string]any) {
		handleMockACPRequest(t, conn, msg, mockACPBehavior{})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := newACPWSSession(ctx, wsURL, "", nil, 2*time.Second, 0, "/remote/project", "", "")
	if err != nil {
		t.Fatalf("newACPWSSession: %v", err)
	}
	defer func() { _ = s.Close() }()

	if got := s.CurrentSessionID(); got != "ws-session" {
		t.Fatalf("CurrentSessionID = %q, want ws-session", got)
	}

	if err := s.Send("hello", nil, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitForEvent(t, s.Events(), func(ev core.Event) bool {
		return ev.Type == core.EventText && ev.Content == "hello from ws"
	})
	waitForEvent(t, s.Events(), func(ev core.Event) bool {
		return ev.Type == core.EventResult && ev.SessionID == "ws-session"
	})
}

func TestACPWSSessionLoad(t *testing.T) {
	loads := make(chan string, 1)
	wsURL := startMockACPWSServer(t, func(conn *websocket.Conn, msg map[string]any) {
		handleMockACPRequest(t, conn, msg, mockACPBehavior{
			onLoad: func(sessionID string) {
				loads <- sessionID
			},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := newACPWSSession(ctx, wsURL, "", nil, 2*time.Second, 0, "/remote/project", "resume-123", "")
	if err != nil {
		t.Fatalf("newACPWSSession: %v", err)
	}
	defer func() { _ = s.Close() }()

	if got := s.CurrentSessionID(); got != "resume-123" {
		t.Fatalf("CurrentSessionID = %q, want resume-123", got)
	}

	select {
	case got := <-loads:
		if got != "resume-123" {
			t.Fatalf("load session = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session/load")
	}
}

func TestACPWSPermissionResponse(t *testing.T) {
	permResp := make(chan map[string]any, 1)
	wsURL := startMockACPWSServer(t, func(conn *websocket.Conn, msg map[string]any) {
		handleMockACPRequest(t, conn, msg, mockACPBehavior{
			onPrompt: func(reqID any) {
				mustWriteWSJSON(t, conn, map[string]any{
					"jsonrpc": "2.0",
					"id":      77,
					"method":  "session/request_permission",
					"params": map[string]any{
						"sessionId": "ws-session",
						"toolCall": map[string]any{
							"toolCallId": "tool-1",
							"title":      "Run command",
							"kind":       "command",
						},
						"options": []map[string]any{
							{"optionId": "allow-1", "kind": "allow_once"},
							{"optionId": "deny-1", "kind": "reject_once"},
						},
					},
				})
				mustWriteWSJSON(t, conn, rpcSuccess(reqID, map[string]any{}))
			},
			onResponse: func(msg map[string]any) {
				permResp <- msg
			},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := newACPWSSession(ctx, wsURL, "", nil, 2*time.Second, 0, "/remote/project", "", "")
	if err != nil {
		t.Fatalf("newACPWSSession: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Send("need permission", nil, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	ev := waitForEvent(t, s.Events(), func(ev core.Event) bool {
		return ev.Type == core.EventPermissionRequest
	})
	if err := s.RespondPermission(ev.RequestID, core.PermissionResult{Behavior: "allow"}); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}

	select {
	case msg := <-permResp:
		resp, _ := msg["result"].(map[string]any)
		outcome, _ := resp["outcome"].(map[string]any)
		if outcome["optionId"] != "allow-1" {
			t.Fatalf("optionId = %v, want allow-1", outcome["optionId"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for permission response")
	}
}

func TestACPWSSendStagesAttachments(t *testing.T) {
	stageCalls := make(chan map[string]any, 4)
	prompts := make(chan string, 1)
	wsURL := startMockACPWSServer(t, func(conn *websocket.Conn, msg map[string]any) {
		handleMockACPRequest(t, conn, msg, mockACPBehavior{
			onStageAttachment: func(params map[string]any) {
				stageCalls <- params
			},
			onPromptText: func(text string) {
				prompts <- text
			},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := newACPWSSession(ctx, wsURL, "", nil, 2*time.Second, 0, "/remote/project", "", "")
	if err != nil {
		t.Fatalf("newACPWSSession: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Send("", []core.ImageAttachment{{MimeType: "image/png", Data: []byte("img")}}, []core.FileAttachment{{FileName: "notes.txt", Data: []byte("file")}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-stageCalls:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for stage attachment call")
		}
	}

	select {
	case prompt := <-prompts:
		if !strings.Contains(prompt, "/remote/project/staged/img_") {
			t.Fatalf("prompt missing staged image path: %q", prompt)
		}
		if !strings.Contains(prompt, "/remote/project/staged/notes.txt") {
			t.Fatalf("prompt missing staged file path: %q", prompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for prompt")
	}
}

func TestACPWSSendAttachmentUnsupported(t *testing.T) {
	wsURL := startMockACPWSServer(t, func(conn *websocket.Conn, msg map[string]any) {
		handleMockACPRequest(t, conn, msg, mockACPBehavior{
			stageUnsupported: true,
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := newACPWSSession(ctx, wsURL, "", nil, 2*time.Second, 0, "/remote/project", "", "")
	if err != nil {
		t.Fatalf("newACPWSSession: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Send("", nil, []core.FileAttachment{{FileName: "notes.txt", Data: []byte("file")}})
	if err == nil || !strings.Contains(err.Error(), "does not support cc/stage_attachment") {
		t.Fatalf("expected unsupported staging error, got %v", err)
	}
}

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "stdio" {
		os.Exit(0)
	}

	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		method, _ := msg["method"].(string)
		id := msg["id"]
		switch method {
		case "initialize":
			_ = enc.Encode(rpcSuccess(id, map[string]any{
				"protocolVersion": 1,
				"agentCapabilities": map[string]any{
					"loadSession": true,
				},
			}))
		case "session/load":
			params, _ := msg["params"].(map[string]any)
			_ = enc.Encode(rpcSuccess(id, map[string]any{"sessionId": params["sessionId"]}))
		case "session/new":
			_ = enc.Encode(rpcSuccess(id, map[string]any{"sessionId": "stdio-session"}))
		case "session/prompt":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": "stdio-session",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{
							"type": "text",
							"text": "hello from stdio",
						},
					},
				},
			})
			_ = enc.Encode(rpcSuccess(id, map[string]any{}))
		case "cc/stage_attachment":
			params, _ := msg["params"].(map[string]any)
			name, _ := params["fileName"].(string)
			_ = enc.Encode(rpcSuccess(id, map[string]any{"path": "/remote/staged/" + name}))
		default:
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not implemented",
				},
			})
		}
	}
	os.Exit(0)
}

type mockACPBehavior struct {
	stageUnsupported  bool
	onLoad            func(sessionID string)
	onPrompt          func(reqID any)
	onPromptText      func(text string)
	onStageAttachment func(params map[string]any)
	onResponse        func(msg map[string]any)
}

func startMockACPWSServer(t *testing.T, handler func(conn *websocket.Conn, msg map[string]any)) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		go func() {
			defer conn.Close()
			for {
				var msg map[string]any
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				handler(conn, msg)
			}
		}()
	}))
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http")
}

func handleMockACPRequest(t *testing.T, conn *websocket.Conn, msg map[string]any, behavior mockACPBehavior) {
	t.Helper()
	method, _ := msg["method"].(string)
	if method == "" {
		if behavior.onResponse != nil {
			behavior.onResponse(msg)
		}
		return
	}
	id := msg["id"]
	switch method {
	case "initialize":
		mustWriteWSJSON(t, conn, rpcSuccess(id, map[string]any{
			"protocolVersion": 1,
			"agentCapabilities": map[string]any{
				"loadSession": true,
			},
		}))
	case "authenticate":
		mustWriteWSJSON(t, conn, rpcSuccess(id, map[string]any{}))
	case "session/load":
		params, _ := msg["params"].(map[string]any)
		sessionID, _ := params["sessionId"].(string)
		if behavior.onLoad != nil {
			behavior.onLoad(sessionID)
		}
		mustWriteWSJSON(t, conn, rpcSuccess(id, map[string]any{"sessionId": sessionID}))
	case "session/new":
		mustWriteWSJSON(t, conn, rpcSuccess(id, map[string]any{"sessionId": "ws-session"}))
	case "cc/stage_attachment":
		if behavior.stageUnsupported {
			mustWriteWSJSON(t, conn, rpcError(id, -32601, "method not implemented"))
			return
		}
		params, _ := msg["params"].(map[string]any)
		if behavior.onStageAttachment != nil {
			behavior.onStageAttachment(params)
		}
		name, _ := params["fileName"].(string)
		workDir, _ := params["workDir"].(string)
		mustWriteWSJSON(t, conn, rpcSuccess(id, map[string]any{"path": workDir + "/staged/" + name}))
	case "session/prompt":
		if behavior.onPrompt != nil {
			behavior.onPrompt(id)
			return
		}
		if behavior.onPromptText != nil {
			behavior.onPromptText(extractPromptText(msg))
		}
		mustWriteWSJSON(t, conn, map[string]any{
			"jsonrpc": "2.0",
			"method":  "session/update",
			"params": map[string]any{
				"sessionId": "ws-session",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content": map[string]any{
						"type": "text",
						"text": "hello from ws",
					},
				},
			},
		})
		mustWriteWSJSON(t, conn, rpcSuccess(id, map[string]any{}))
	default:
		mustWriteWSJSON(t, conn, rpcError(id, -32601, "method not implemented"))
	}
}

func extractPromptText(msg map[string]any) string {
	params, _ := msg["params"].(map[string]any)
	blocks, _ := params["prompt"].([]any)
	if len(blocks) == 0 {
		return ""
	}
	first, _ := blocks[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func rpcSuccess(id any, result any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}

func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
}

func mustWriteWSJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("write websocket json: %v", err)
	}
}

func waitForEvent(t *testing.T, events <-chan core.Event, match func(core.Event) bool) core.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed unexpectedly")
			}
			if match(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("timed out waiting for event")
		}
	}
}
