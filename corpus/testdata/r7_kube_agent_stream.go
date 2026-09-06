// From kite-org/kite: pkg/ai/anthropic.go, with the system prompt and
// conversation loop from pkg/ai/agent.go, the tool table and
// AnthropicToolDefs from pkg/ai/tools.go, and the provider default from
// pkg/model/general_setting.go, which is where the repository keeps it.
// The OpenAI provider branch, the pending-session pause, and all but
// five of the tools are dropped; the streaming consumer is stubbed.

package ai

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

const DefaultGeneralAnthropicModel = "claude-sonnet-4-5"

const maxAgentIterations = 100

const systemPrompt = `You are Kite AI, an intelligent assistant for Kubernetes cluster management. You help users understand, monitor, and manage their Kubernetes clusters safely and accurately.

You have access to tools that let you interact with the user's Kubernetes cluster. Use them to:
- Get information about specific resources (pods, deployments, services, etc.)
- List resources across namespaces
- Read pod logs for debugging
- Run a one-off non-interactive command in a Pod container when structured tools and logs are insufficient
- Get cluster-wide status overviews
- Query Prometheus metrics for monitoring data (requires cluster-wide read access)
- Create, update, patch or delete resources

Operating principles:
- Evidence first: collect relevant cluster state before conclusions. Do not guess cluster state.
- Read before write: before any mutation operation (create/update/patch/delete), inspect current related resources unless the request is an explicit create with complete details.
- Verify after write: after a mutation, re-check the affected resource(s) and report whether the change actually took effect.
- Scope safety: prefer the smallest safe scope; avoid broad or destructive actions unless the user explicitly asks for them.
- Pod exec safety: prefer read-only diagnostic commands. Do not use exec when a structured resource or log tool can answer the question.

Context priority:
- Follow explicit user instructions first.
- If user intent does not specify scope, use current page context (resource/namespace) as default scope.
- If scope is still unclear, ask a concise clarification question before mutating resources.

Creation and mutation guardrails:
- For mutation operations (create/update/patch/delete), always include a brief text explanation of what you are about to do alongside the tool call so the user can confirm.
- For create operations, do not assume critical defaults. If missing, ask for required details such as namespace, image/tag, ports/exposure, storage, resource requests/limits, and required config/secrets.
- Do not output secret values. If sensitive fields are involved, summarize safely.

Failure handling:
- On Forbidden errors, explain the permission boundary and provide a least-privilege next step.
- If a tool returns Forbidden, do not retry the same verb/resource/scope. Choose a permitted scope or ask for RBAC changes.
- On NotFound errors, confirm namespace/kind/name and suggest nearby resources when possible.
- On validation or apply errors, explain the failing field and provide a minimal fix.
`

type agentToolDefinition struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

func toolDefinitions() []agentToolDefinition {
	return []agentToolDefinition{
		{
			Name:        "get_resource",
			Description: "Get one Kubernetes resource as YAML. Pass kind, name, and namespace as separate fields; never put namespace in name.",
			Properties: map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"description": "Resource kind, such as Pod, Deployment, Service, Node, or widgets.example.com.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Resource name without a namespace prefix.",
				},
				"namespace": map[string]any{
					"type":        "string",
					"description": "Namespace for a namespaced resource. Omit for cluster-scoped resources.",
				},
			},
			Required: []string{"kind", "name"},
		},
		{
			Name:        "list_resources",
			Description: "List Kubernetes resources and return a compact diagnostic summary. Use get_resource when the full YAML of one resource is needed.",
			Properties: map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"description": "Resource kind, such as Pod, Deployment, Service, Node, Event, or widgets.example.com.",
				},
				"namespace": map[string]any{
					"type":        "string",
					"description": "Namespace to list. Omit to list all namespaces or for cluster-scoped resources.",
				},
				"label_selector": map[string]any{
					"type":        "string",
					"description": "Optional Kubernetes label selector, such as app=nginx.",
				},
			},
			Required: []string{"kind"},
		},
		{
			Name:        "get_pod_logs",
			Description: "Get recent logs from one Pod. The pod name and namespace are separate required fields.",
			Properties: map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Pod name without a namespace prefix.",
				},
				"namespace": map[string]any{
					"type":        "string",
					"description": "Pod namespace.",
				},
				"container": map[string]any{
					"type":        "string",
					"description": "Optional container name.",
				},
				"tail_lines": map[string]any{
					"type":        "integer",
					"description": "Number of recent lines. Defaults to 100.",
				},
			},
			Required: []string{"name", "namespace"},
		},
		{
			Name:        "get_cluster_overview",
			Description: "Get a compact overview of cluster nodes, pods, namespaces, and services. Use this as the first step for a broad cluster health check.",
			Properties:  map[string]any{},
		},
		{
			Name:        "apply_resource",
			Description: "Create or update one Kubernetes resource with Server-Side Apply. Pass one complete YAML document. This is a mutation and requires user confirmation.",
			Properties: map[string]any{
				"yaml": map[string]any{
					"type":        "string",
					"description": "One complete Kubernetes resource manifest in YAML.",
				},
			},
			Required: []string{"yaml"},
		},
	}
}

func AnthropicToolDefs(defs []agentToolDefinition) []anthropic.ToolUnionParam {
	tools := make([]anthropic.ToolUnionParam, 0, len(defs))

	for _, def := range defs {
		tool := anthropic.ToolParam{
			Name:        def.Name,
			Description: anthropic.String(def.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Type:       "object",
				Properties: def.Properties,
				Required:   def.Required,
			},
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &tool})
	}

	return tools
}

type providerRequest struct {
	SystemPrompt string
	Messages     []AgentMessage
	Tools        []agentToolDefinition
	Model        string
	MaxTokens    int
}

type modelProvider interface {
	Stream(context.Context, providerRequest, func(AgentEvent)) (AgentMessage, error)
}

// RuntimeConfig is what the settings table resolves to at startup.
type RuntimeConfig struct {
	Provider  string
	Model     string
	MaxTokens int
}

func loadRuntimeConfig(aiModel string, aiMaxTokens int) *RuntimeConfig {
	cfg := &RuntimeConfig{
		Provider:  "anthropic",
		Model:     strings.TrimSpace(aiModel),
		MaxTokens: aiMaxTokens,
	}
	if cfg.Model == "" {
		cfg.Model = DefaultGeneralAnthropicModel
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 16384
	}
	return cfg
}

type anthropicProvider struct {
	client anthropic.Client
}

func (p *anthropicProvider) Stream(
	ctx context.Context,
	request providerRequest,
	sendEvent func(AgentEvent),
) (AgentMessage, error) {
	stream := p.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     request.Model,
		Messages:  toAnthropicMessages(request.Messages),
		System:    []anthropic.TextBlockParam{{Text: request.SystemPrompt}},
		Tools:     AnthropicToolDefs(request.Tools),
		MaxTokens: int64(request.MaxTokens),
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{},
		},
	})
	return consumeAnthropicStreamingResponse(stream, sendEvent)
}

func toAnthropicMessages(messages []AgentMessage) []anthropic.MessageParam {
	params := make([]anthropic.MessageParam, 0, len(messages))
	for _, message := range messages {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(message.Content))
		for _, block := range message.Content {
			switch block.Type {
			case contentBlockText:
				blocks = append(blocks, anthropic.NewTextBlock(block.Text))
			case contentBlockToolCall:
				blocks = append(blocks, anthropic.NewToolUseBlock(block.ToolCallID, block.Arguments, block.ToolName))
			case contentBlockToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(block.ToolCallID, block.Text, block.IsError))
			}
		}
		if len(blocks) == 0 {
			continue
		}
		if message.Role == messageRoleAssistant {
			params = append(params, anthropic.NewAssistantMessage(blocks...))
		} else {
			params = append(params, anthropic.NewUserMessage(blocks...))
		}
	}
	return params
}

// Agent owns one provider and the resolved model and cap for the session.
type Agent struct {
	provider  modelProvider
	model     string
	maxTokens int
}

func NewAgent(cfg *RuntimeConfig, client anthropic.Client) *Agent {
	return &Agent{provider: &anthropicProvider{client: client}, model: cfg.Model, maxTokens: cfg.MaxTokens}
}

func (a *Agent) runConversation(
	ctx context.Context,
	systemPrompt string,
	messages []AgentMessage,
	startIteration int,
	sendEvent func(AgentEvent),
) {
	tools := toolDefinitions()
	for iteration := startIteration; iteration < maxAgentIterations; iteration++ {
		message, err := a.provider.Stream(ctx, providerRequest{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        tools,
			Model:        a.model,
			MaxTokens:    a.maxTokens,
		}, sendEvent)
		if err != nil {
			sendEvent(AgentEvent{Type: "error", Data: ErrorEvent{Message: fmt.Sprintf("AI error: %v", err)}})
			return
		}
		if !message.hasContent() {
			sendEvent(AgentEvent{Type: "error", Data: ErrorEvent{Message: "AI returned no content"}})
			return
		}

		messages = append(messages, message)
		sendEvent(AgentEvent{Type: "message_end", Data: MessageEndEvent{Message: message}})
		toolCalls := message.toolCalls()
		if len(toolCalls) == 0 {
			return
		}

		for _, toolCall := range toolCalls {
			sendEvent(AgentEvent{Type: "tool_call", Data: ToolCallEvent{ToolCall: toolCall}})
		}

		messages = a.processToolBatch(ctx, messages, toolCalls, sendEvent)
	}

	sendEvent(AgentEvent{Type: "error", Data: ErrorEvent{Message: "Too many tool calling iterations"}})
}

func consumeAnthropicStreamingResponse(stream interface {
	Next() bool
	Current() anthropic.MessageStreamEventUnion
	Err() error
	Close() error
}, sendEvent func(AgentEvent)) (AgentMessage, error) {
	panic("not implemented")
}

func (a *Agent) processToolBatch(ctx context.Context, messages []AgentMessage, toolCalls []ToolCall, sendEvent func(AgentEvent)) []AgentMessage {
	panic("not implemented")
}
