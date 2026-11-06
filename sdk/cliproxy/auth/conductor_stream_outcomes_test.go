package auth

import (
	"context"
	"io"
	"testing"

	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestWrapStreamResultResponseOutcomes(t *testing.T) {
	completed := func(id string) core.StreamChunk {
		return core.StreamChunk{Payload: []byte(`{"type":"response.completed"}`), Outcome: &core.StreamResponseOutcome{ResponseID: id, Completed: true}}
	}
	for _, scenario := range []struct {
		name        string
		chunks      []core.StreamChunk
		wantSuccess []bool
	}{
		{
			name:        "duplicate_terminal_counts_once",
			chunks:      []core.StreamChunk{completed("r1"), completed("r1"), completed("r2")},
			wantSuccess: []bool{true, true},
		},
		{
			name: "business_failure_does_not_hide_later_success",
			chunks: []core.StreamChunk{
				{Payload: []byte(`{"type":"response.failed"}`), Outcome: &core.StreamResponseOutcome{ResponseID: "r1", Err: customStatusError{code: 400, msg: "rejected"}}},
				completed("r2"),
				{Err: io.EOF, ConnectionOnly: true},
			},
			wantSuccess: []bool{false, true},
		},
		{
			name:        "active_loss_preserves_prior_success",
			chunks:      []core.StreamChunk{completed("r1"), {Err: io.EOF}},
			wantSuccess: []bool{true, false},
		},
		{
			name:        "ordinary_stream_retains_one_result",
			chunks:      []core.StreamChunk{{Payload: []byte(`{"type":"response.completed"}`)}, {Payload: []byte(`{"type":"response.completed"}`)}},
			wantSuccess: []bool{true},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			hook := &captureResultHook{}
			manager := NewManager(nil, nil, hook)
			credential, err := manager.Register(WithSkipPersist(context.Background()), &Auth{ID: t.Name(), Provider: "codex", Status: StatusActive})
			if err != nil {
				t.Fatal(err)
			}
			chunks := make(chan core.StreamChunk, len(scenario.chunks))
			for _, chunk := range scenario.chunks {
				chunks <- chunk
			}
			close(chunks)
			stream := manager.wrapStreamResult(context.Background(), credential, "codex", "model", "model", nil, nil, chunks, OAuthModelAliasResult{}, false, core.Options{}, 1)
			for range stream.Chunks {
			}
			if len(hook.results) != len(scenario.wantSuccess) {
				t.Fatalf("recorded %d outcomes, want %d: %+v", len(hook.results), len(scenario.wantSuccess), hook.results)
			}
			for i, want := range scenario.wantSuccess {
				result := hook.results[i]
				if result.Success != want || result.CompletedResponse != want {
					t.Errorf("outcome %d: success=%t completed=%t, want %t", i, result.Success, result.CompletedResponse, want)
				}
			}
		})
	}
}
