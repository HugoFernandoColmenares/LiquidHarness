// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/ports"
)

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)

// Registry is the tool catalog of one workspace. It is mutable during construction and
// read-only afterwards: the agent loop reads it from several goroutines, so Register must not
// be called once a turn has started.
type Registry struct {
	tools map[string]ports.Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]ports.Tool)}
}

// Register validates and adds a tool. Re-registering a name is an error: two tools with the
// same name would make the model's choice ambiguous.
func (r *Registry) Register(tool ports.Tool) error {
	if tool == nil {
		return fmt.Errorf("tools: nil tool")
	}
	spec := tool.Spec()
	if !toolNamePattern.MatchString(spec.Name) {
		return fmt.Errorf("tools: invalid name %q", spec.Name)
	}
	if strings.TrimSpace(spec.Description) == "" {
		return fmt.Errorf("tools: %s has an empty description", spec.Name)
	}
	if !spec.Risk.Valid() {
		return fmt.Errorf("tools: %s has an invalid risk class %d", spec.Name, spec.Risk)
	}
	if !json.Valid(spec.InputSchema) {
		return fmt.Errorf("tools: %s has an invalid input schema", spec.Name)
	}
	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("tools: %s is already registered", spec.Name)
	}
	r.tools[spec.Name] = tool
	return nil
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (ports.Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// Names returns the registered names sorted alphabetically.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Specs returns the specs sorted by name, which keeps prompts and eval fixtures stable.
func (r *Registry) Specs() []core.ToolSpec {
	specs := make([]core.ToolSpec, 0, len(r.tools))
	for _, name := range r.Names() {
		specs = append(specs, r.tools[name].Spec())
	}
	return specs
}

// Len returns the number of registered tools.
func (r *Registry) Len() int { return len(r.tools) }

// Options bounds the write tools of a workspace.
type Options struct {
	// MaxWriteBytes caps fs_create content and fs_edit results. Zero means 1 MiB.
	MaxWriteBytes int64
}

// NewReadOnly builds the F2 catalog: fs_read, fs_list and fs_search, all confined to root.
func NewReadOnly(root string) (*Registry, error) {
	registry := NewRegistry()
	for _, tool := range []ports.Tool{
		newFSRead(root),
		newFSList(root),
		newFSSearch(root),
	} {
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// NewWorkspace builds the F3 catalog: the read-only tools plus fs_create, fs_edit, fs_delete,
// fs_move and todo_write. Callers in read-only mode must use NewReadOnly so the model never sees
// a write tool it cannot use.
func NewWorkspace(root string, opts Options) (*Registry, error) {
	registry := NewRegistry()
	for _, tool := range []ports.Tool{
		newFSRead(root),
		newFSList(root),
		newFSSearch(root),
		newFSCreate(root, opts.MaxWriteBytes),
		newFSEdit(root, opts.MaxWriteBytes),
		newFSDelete(root),
		newFSMove(root),
		newTodoWrite(),
	} {
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
