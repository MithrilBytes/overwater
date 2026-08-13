package catalog

import (
	"sort"
	"strings"
)

// The other direction: models LiteLLM prices that this catalog does not
// carry.
//
// DiffLitellm answers "are our prices still right". This answers "what
// are we blind to", which is the question a scan raises. A repository
// pinning deepseek-v4-flash gets no price, no finding and no candidate,
// and the only honest thing the scanner can say is that it does not know
// the model. Upstream does know it, and has for a while.
//
// Upstream lists the same model many times over, once per host that
// serves it: azure_ai/deepseek-v4-flash, dashscope/deepseek-v4-flash and
// us.anthropic.claude-opus-4-7 are three routes to two models. Reporting
// every key would bury the answer, so keys collapse to the bare id and
// the routes that reached it ride along as evidence.

// Unlisted is one model upstream prices and this catalog lacks.
type Unlisted struct {
	ID        string  // the bare id, provider routing stripped
	Provider  string  // litellm_provider of the first key seen
	Mode      string  // chat, embedding, rerank, and so on
	Input     float64 // dollars per million input tokens
	Output    float64
	HasOutput bool
	MaxInput  int
	Keys      []string // the upstream keys that collapsed to this id
}

// maxSanePricePerMtok rejects an upstream record whose units are wrong
// rather than whose model is expensive. Every entry above this is a
// wandb key listing prices a thousandfold high; the dearest model
// anyone actually sells is under a hundred dollars per million tokens,
// and this catalog's own maximum is 75.
const maxSanePricePerMtok = 500.0

// Modes worth carrying. Image, audio and video generation price per
// asset rather than per token, and nothing in the rules engine reasons
// about them.
var wantedModes = map[string]bool{
	"chat": true, "completion": true, "responses": true,
	"embedding": true, "rerank": true, "": true,
}

// hostPrefixes route a model through somewhere. Stripping them is what
// turns 3,020 upstream keys into a list a person can read.
var hostPrefixes = []string{
	"azure_ai/", "azure/", "bedrock/", "bedrock_converse/", "vertex_ai/",
	"vertex_ai-language-models/", "gemini/", "openrouter/", "fireworks_ai/",
	"fireworks_ai-models/", "deepinfra/", "novita/", "together_ai/",
	"vercel_ai_gateway/", "dashscope/", "groq/", "cerebras/", "sambanova/",
	"perplexity/", "anyscale/", "voyage/", "mistral/", "codestral/",
	"cloudflare/", "ollama/", "ollama_chat/", "friendliai/", "nscale/",
	"jina_ai/", "databricks/", "watsonx/", "nebius/", "featherless_ai/",
	"snowflake/", "lambda_ai/", "hyperbolic/", "moonshot/", "volcengine/",
	"meta_llama/", "nvidia_nim/", "xai/", "aiohttp_openai/", "litellm_proxy/",
}

// Regional and vendor dotted prefixes: us.anthropic.claude-opus-4-7.
var dotPrefixes = []string{
	"us.", "eu.", "apac.", "global.", "anthropic.", "meta.", "amazon.",
	"mistral.", "cohere.", "ai21.", "deepseek.", "qwen.", "openai.",
}

// bareID strips routing so the same model under six hosts is one entry.
func bareID(key string) string {
	id := key
	for changed := true; changed; {
		changed = false
		for _, p := range hostPrefixes {
			if len(id) > len(p) && strings.EqualFold(id[:len(p)], p) {
				id, changed = id[len(p):], true
			}
		}
	}
	// Whatever routing remains before the last slash is a host we have
	// not named; the id is the final segment.
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	for changed := true; changed; {
		changed = false
		for _, p := range dotPrefixes {
			if len(id) > len(p) && strings.EqualFold(id[:len(p)], p) {
				id, changed = id[len(p):], true
			}
		}
	}
	return id
}

// ReverseDiff reports models LiteLLM prices that the catalog does not
// carry, one entry per bare id. A model we already have under any id or
// alias is not reported, whatever route upstream lists it under.
//
// onlyIDs, when not empty, narrows the answer to those bare ids: it is
// how a scan's unrecognised models become a list of entries to add.
func ReverseDiff(c *Catalog, prices LitellmPrices, onlyIDs []string) []Unlisted {
	have := map[string]bool{}
	for _, m := range c.Models {
		have[strings.ToLower(m.ID)] = true
		have[strings.ToLower(bareID(m.ID))] = true
		for _, a := range m.Aliases {
			have[strings.ToLower(a)] = true
			have[strings.ToLower(bareID(a))] = true
		}
	}
	want := map[string]bool{}
	for _, id := range onlyIDs {
		want[strings.ToLower(bareID(id))] = true
	}

	byID := map[string]*Unlisted{}
	for key, p := range prices {
		if !wantedModes[p.Mode] {
			continue
		}
		if p.Input > maxSanePricePerMtok || p.Output > maxSanePricePerMtok {
			continue
		}
		id := bareID(key)
		low := strings.ToLower(id)
		if id == "" || have[low] {
			continue
		}
		if len(want) > 0 && !want[low] {
			continue
		}
		u, seen := byID[low]
		if !seen {
			byID[low] = &Unlisted{
				ID: id, Provider: p.Provider, Mode: p.Mode,
				Input: p.Input, Output: p.Output, HasOutput: p.HasOutput,
				MaxInput: p.MaxInput, Keys: []string{key},
			}
			continue
		}
		u.Keys = append(u.Keys, key)
		// Prefer the route that carries the most detail.
		if !u.HasOutput && p.HasOutput {
			u.Output, u.HasOutput = p.Output, true
		}
		if u.MaxInput == 0 {
			u.MaxInput = p.MaxInput
		}
		if u.Provider == "" {
			u.Provider = p.Provider
		}
	}

	out := make([]Unlisted, 0, len(byID))
	for _, u := range byID {
		sort.Strings(u.Keys)
		out = append(out, *u)
	}
	// Most expensive first: a model nobody can price is worth adding in
	// the order it would cost somebody money.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Input != out[j].Input {
			return out[i].Input > out[j].Input
		}
		return out[i].ID < out[j].ID
	})
	return out
}
