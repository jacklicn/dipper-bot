package tools

import (
	"context"
	"encoding/json"
	"sync"
)

// Registry holds and executes tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Definitions returns all tools in OpenAI format.
func (r *Registry) Definitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := len(r.tools)
	out := make([]map[string]any, 0, n)
	for _, t := range r.tools {
		out = append(out, Schema(t))
	}
	return out
}

// Execute runs a tool by name with the given params (from LLM JSON).
func (r *Registry) Execute(ctx context.Context, name string, params map[string]any) (string, error) {
	r.mu.RLock()
	t := r.tools[name]
	r.mu.RUnlock()
	if t == nil {
		return "Error: Tool '" + name + "' not found", nil
	}
	// params may have string values that need to be parsed (e.g. from JSON)
	normalized := normalizeParams(params, t.Parameters())
	return t.Execute(ctx, normalized)
}

func normalizeParams(params, schema map[string]any) map[string]any {
	if params == nil {
		return map[string]any{}
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return params
	}
	out := make(map[string]any)
	for k, v := range params {
		if prop, ok := props[k].(map[string]any); ok {
			if s, ok := v.(string); ok && prop["type"] == "object" {
				var m map[string]any
				if json.Unmarshal([]byte(s), &m) == nil {
					v = m
				}
			}
			if typ, _ := prop["type"].(string); typ == "string" {
				v = CoerceStringParam(v)
			}
		}
		out[k] = v
	}
	return out
}
