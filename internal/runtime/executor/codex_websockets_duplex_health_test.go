package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	auth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	translator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCodexDuplexResponseAccountingAfterConnectionLoss(t *testing.T) {
	readTimeout := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
	for _, scenario := range []struct {
		name                    string
		loss                    error
		active, rejected        bool
		wantSuccess, wantFailed int64
	}{
		{name: "idle_timeout", loss: readTimeout, wantSuccess: 2},
		{name: "idle_eof", loss: io.EOF, wantSuccess: 2},
		{name: "active_timeout", loss: readTimeout, active: true, wantSuccess: 1, wantFailed: 1},
		{name: "business_rejection_then_eof", loss: io.EOF, rejected: true, wantSuccess: 1, wantFailed: 1},
		{name: "business_rejection_then_wrapped_eof", loss: &core.UpstreamWebsocketTurnError{Cause: io.EOF}, rejected: true, wantSuccess: 1, wantFailed: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = c.Close() }()
				for turn := 1; turn <= 2; turn++ {
					if _, _, err = c.ReadMessage(); err != nil {
						t.Error(err)
						return
					}
					write := func(kind string) bool {
						payload := fmt.Sprintf(`{"type":%q,"response":{"id":"r%d","output":[],"error":{"type":"invalid_request_error","message":"rejected"}}}`, kind, turn)
						if errWrite := c.WriteMessage(websocket.TextMessage, []byte(payload)); errWrite != nil {
							t.Error(errWrite)
							return false
						}
						return true
					}
					if !write("response.created") {
						return
					}
					if turn == 2 && scenario.active {
						break
					}
					terminal := "response.completed"
					if turn == 2 && scenario.rejected {
						terminal = "response.failed"
					}
					if !write(terminal) {
						return
					}
				}
				_, _, _ = c.ReadMessage()
			}))
			defer upstream.Close()
			cfg := &config.Config{}
			cfg.Codex.ResponseSteering = true
			cfg.CodexResponseSteering = true
			manager := auth.NewManager(nil, nil, nil)
			manager.SetConfig(cfg)
			manager.RegisterExecutor(NewCodexAutoExecutor(cfg))
			id, model := "duplex-health-"+scenario.name, "duplex-health-model"
			_, err := manager.Register(context.Background(), &auth.Auth{ID: id, Provider: "codex", Status: auth.StatusActive, Attributes: map[string]string{"api_key": "test-key", "base_url": upstream.URL, "websockets": "true"}})
			if err != nil {
				t.Fatal(err)
			}
			registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: model}})
			defer registry.GetGlobalRegistry().UnregisterClient(id)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			input := make(chan core.WebsocketInput, 1)
			ctx = core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input)
			request := []byte(`{"type":"response.create","model":"duplex-health-model","input":[]}`)
			stream, err := manager.ExecuteStream(ctx, []string{"codex"}, core.Request{Model: model, Payload: request}, core.Options{SourceFormat: translator.FromString("codex"), Metadata: map[string]any{core.ExecutionSessionMetadataKey: t.Name()}})
			if err != nil {
				t.Fatal(err)
			}
			completed, sawLoss := int64(0), false
			for chunk := range stream.Chunks {
				kind := ojson.GetBytes(chunk.Payload, "type").String()
				responseID := ojson.GetBytes(chunk.Payload, "response.id").String()
				if kind == "response.completed" {
					completed++
					current, _ := manager.GetByID(id)
					if current.Success != completed {
						t.Fatalf("successes while connection is open = %d, want %d", current.Success, completed)
					}
					if responseID == "r1" {
						input <- core.WebsocketInput{Payload: request}
					}
				}
				if responseID == "r2" && ((scenario.active && kind == "response.created") || kind == "response.completed" || kind == "response.failed") {
					// Inject transport loss deterministically without waiting for a network deadline.
					input <- core.WebsocketInput{Err: scenario.loss}
				}
				if chunk.Err != nil {
					sawLoss = true
					if scenario.rejected && (core.IsUpstreamWebsocketTurnError(chunk.Err) || !errors.Is(chunk.Err, io.EOF)) {
						t.Fatalf("rejected response closure = %v, want terminal EOF without a recoverable loss marker", chunk.Err)
					}
					if !scenario.rejected {
						var turnErr *core.UpstreamWebsocketTurnError
						if !errors.As(chunk.Err, &turnErr) || turnErr.Idle == scenario.active || !errors.Is(chunk.Err, scenario.loss) {
							t.Fatalf("connection loss = %#v, want recoverable loss with idle=%t", chunk.Err, !scenario.active)
						}
					}
				}
			}
			if !sawLoss {
				t.Fatal("connection loss was not forwarded")
			}
			current, ok := manager.GetByID(id)
			if !ok {
				t.Fatal("account disappeared")
			}
			if current.Success != scenario.wantSuccess || current.Failed != scenario.wantFailed {
				t.Fatalf("success=%d failed=%d, want %d/%d", current.Success, current.Failed, scenario.wantSuccess, scenario.wantFailed)
			}
			if current.Unavailable {
				t.Fatal("connection loss or business rejection cooled the account")
			}
			if state := current.ModelStates[model]; state != nil && state.Unavailable {
				t.Fatalf("connection loss or business rejection cooled the model: %+v", state.LastError)
			}
		})
	}
}
