// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
)

// todoWrite stores the plan the model is following. It never touches the filesystem; F4 will
// persist it with the session.
type todoWrite struct {
	spec core.ToolSpec

	mu    sync.Mutex
	items []todoItem
}

type todoItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

type todoArgs struct {
	Items []todoItem `json:"items"`
}

type todoData struct {
	Items     []todoItem `json:"items"`
	Pending   int        `json:"pending"`
	Completed int        `json:"completed"`
}

var todoStatuses = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"done":        true,
}

func newTodoWrite() *todoWrite {
	return &todoWrite{
		spec: core.ToolSpec{
			Name: "todo_write",
			Description: "Replace the task list for the current turn. Use it to plan multi-step " +
				"work and keep the user informed; it does not modify the workspace.",
			InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "maxItems": 50,
      "items": {
        "type": "object",
        "properties": {
          "text": {"type": "string", "description": "Task description"},
          "status": {"type": "string", "enum": ["pending", "in_progress", "done"]}
        },
        "required": ["text", "status"],
        "additionalProperties": false
      }
    }
  },
  "required": ["items"],
  "additionalProperties": false
}`),
			Risk: core.RiskR0,
		},
	}
}

func (t *todoWrite) Spec() core.ToolSpec { return t.spec }

func (t *todoWrite) Execute(_ context.Context, call core.ToolCall) (core.ToolResult, error) {
	started := time.Now()

	var args todoArgs
	if toolErr := decodeArgs(call, &args); toolErr != nil {
		return newFailure(call, toolErr, started), nil
	}
	if len(args.Items) > 50 {
		return newFailure(call, &core.ToolError{
			Code:    errInvalidArguments,
			Message: "todo_write accepts at most 50 items",
		}, started), nil
	}

	for i, item := range args.Items {
		if strings.TrimSpace(item.Text) == "" {
			return newFailure(call, &core.ToolError{
				Code:    errInvalidArguments,
				Message: fmt.Sprintf("item %d has an empty text", i+1),
			}, started), nil
		}
		if !todoStatuses[item.Status] {
			return newFailure(call, &core.ToolError{
				Code:    errInvalidArguments,
				Message: fmt.Sprintf("item %d has invalid status %q", i+1, item.Status),
				Hint:    "use pending, in_progress or done",
			}, started), nil
		}
	}

	items := append([]todoItem{}, args.Items...)
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()

	data := todoData{Items: items}
	for _, item := range items {
		if item.Status == "done" {
			data.Completed++
			continue
		}
		data.Pending++
	}
	return newResult(call, data, false, started), nil
}
