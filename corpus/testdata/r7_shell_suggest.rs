// From lasantosr/intelli-shell: src/ai/anthropic.rs, with the
// AnthropicModelConfig from src/config.rs, the generate_command_suggestions
// wrapper from src/ai/mod.rs, and the default `suggest` prompt from
// default_config.toml, which is where the repository keeps its prompts.
// The catalog entry the TOML assigns to `suggest` is gemini-flash-latest,
// which is not in the catalog; the Anthropic provider is shown with
// claude-sonnet-4-0, the id the repository's own test configures for it.
// The fallback provider path and the ##...## context placeholders are
// dropped.

use std::fmt::Debug;

use color_eyre::eyre::Context;
use reqwest::{Client, RequestBuilder, Response, header::HeaderName};
use schemars::{JsonSchema, Schema, schema_for};
use serde::{Deserialize, de::DeserializeOwned};
use serde_json::{Value as Json, json};

use crate::errors::{Result, UserFacingError};

const TOOL_NAME: &str = "propose_response";

/// Prompt used to generate command templates from natural language
pub const SUGGEST_PROMPT: &str = r#"
### Instructions
You are an expert CLI assistant. Your task is to generate shell command templates based on the user's request.

Your entire response MUST be a single, valid JSON object conforming to the provided schema and nothing else.

### Shell Paradigm, Syntax, and Versioning
**This is the most important instruction.** Shells have fundamentally different syntaxes, data models, and features depending on their family and version. You MUST adhere strictly to these constraints.

1. **Recognize the Shell Paradigm:**
   - **POSIX / Text-Stream (bash, zsh, fish):** Operate on **text streams**. Use tools like `grep`, `sed`, `awk`.
   - **Object-Pipeline (PowerShell, Nushell):** Operate on **structured data (objects)**. You MUST use internal commands for filtering/selection. AVOID external text-processing tools.
   - **Legacy (cmd.exe):** Has unique syntax for loops (`FOR`), variables (`%VAR%`), and filtering (`findstr`).

2. **Generate Idiomatic Code:**
   - Use the shell's built-in features and standard library.
   - Follow the shell's naming and style conventions (e.g., `Verb-Noun` in PowerShell).
   - Leverage the shell's core strengths (e.g., object manipulation in Nushell).

3. **Ensure Syntactic Correctness:**
   - Pay close attention to variable syntax (`$var`, `$env:VAR`, `$env.VAR`, `%VAR%`).
   - Use the correct operators and quoting rules for the target shell.

4. **Pay Critical Attention to the Version:**
   - The shell version is a primary constraint, not a suggestion. This is especially true for shells with rapid development cycles like **Nushell**.
   - You **MUST** generate commands that are compatible with the user's specified version.
   - Be aware of **breaking changes**. If a command was renamed, replaced, or deprecated in the user's version, you MUST provide the modern, correct equivalent.

### Command Template Syntax
When creating the `command` template string, you must use the following placeholder syntax:

- **Standard Placeholder**: `{{variable-name}}`
  - Use for regular arguments that the user needs to provide.
  - _Example_: `echo "Hello, {{user-name}}!"`

- **Choice Placeholder**: `{{option1|option2}}`
  - Use when the user must choose from a specific set of options.
  - _Example_: `git reset {{--soft|--hard}} HEAD~1`

- **Function Placeholder**: `{{variable:function}}`
  - Use to apply a transformation function to the user's input. Multiple functions can be chained (e.g., `{{variable:snake:upper}}`).
  - Allowed functions: `kebab`, `snake`, `upper`, `lower`, `url`.
  - _Example_: For a user input of "My New Feature", `git checkout -b {{branch-name:kebab}}` would produce `git checkout -b my-new-feature`.

- **Secret/Ephemeral Placeholder**: `{{{...}}}`
  - Use triple curly braces for sensitive values (like API keys, passwords) or for ephemeral content (like a commit message or a description).
    This syntax can wrap any of the placeholder types above.
  - _Example_: `export GITHUB_TOKEN={{{api-key}}}` or `git commit -m "{{{message}}}"`

### Suggestion Strategy
Your primary goal is to provide the most relevant and comprehensive set of command templates. Adhere strictly to the following principles when deciding how many suggestions to provide:

1. **Explicit Single Suggestion:**
   - If the user's request explicitly asks for **a single suggestion**, you **MUST** return a list containing exactly one suggestion object.
   - To cover variations within this single command, make effective use of choice placeholders (e.g., `git reset {{--soft|--hard}}`).

2. **Clear & Unambiguous Request:**
   - If the request is straightforward and has one primary, standard solution, provide a **single, well-formed suggestion**.

3. **Ambiguous or Multi-faceted Request:**
   - If a request is ambiguous, has multiple valid interpretations, or can be solved using several distinct tools or methods, you **MUST provide a comprehensive list of suggestions**.
   - Each distinct approach or interpretation **must be a separate suggestion object**.
   - **Be comprehensive and do not limit your suggestions**. For example, a request for "undo a git commit" could mean `git reset`, `git revert`, or `git checkout`. A request to "find files" could yield suggestions for `find`, `fd`, and `locate`. Provide all valid, distinct alternatives.
   - **Order the suggestions by relevance**, with the most common or recommended solution appearing first.
"#;

/// Configuration for connecting to the Anthropic API
#[derive(Clone, Deserialize)]
#[cfg_attr(test, derive(Debug, PartialEq))]
pub struct AnthropicModelConfig {
    /// The exact model identifier to use (e.g., "claude-sonnet-4-0")
    pub model: String,
    /// The base URL of the API endpoint. Defaults to the official Anthropic API
    #[serde(default = "default_anthropic_url")]
    pub url: String,
    /// The name of the environment variable containing the API key for this model. Defaults to `ANTHROPIC_API_KEY`.
    #[serde(default = "default_anthropic_api_key_env")]
    pub api_key_env: String,
}
fn default_anthropic_url() -> String {
    "https://api.anthropic.com/v1".to_string()
}
fn default_anthropic_api_key_env() -> String {
    "ANTHROPIC_API_KEY".to_string()
}

/// A single command suggestion
#[derive(Debug, Deserialize, JsonSchema)]
pub struct CommandSuggestion {
    /// A short description of what the command does
    pub description: String,
    /// The command template, using the placeholder syntax
    pub command: String,
}

/// The structured response for a suggestion request
#[derive(Debug, Deserialize, JsonSchema)]
pub struct CommandSuggestions {
    pub suggestions: Vec<CommandSuggestion>,
}

pub trait AiProviderBase {
    fn provider_name(&self) -> &'static str;
    fn auth_header(&self, api_key: String) -> (HeaderName, String);
    fn api_key_env_var_name(&self) -> &str;
    fn build_request(
        &self,
        client: &Client,
        sys_prompt: &str,
        user_prompt: &str,
        json_schema: &Schema,
    ) -> RequestBuilder;
}

pub trait AiProvider: AiProviderBase {
    async fn parse_response<T>(&self, res: Response) -> Result<T>
    where
        T: DeserializeOwned + JsonSchema + Debug;
}

impl AiProviderBase for AnthropicModelConfig {
    fn provider_name(&self) -> &'static str {
        "Anthropic"
    }

    fn auth_header(&self, api_key: String) -> (HeaderName, String) {
        (HeaderName::from_static("x-api-key"), api_key)
    }

    fn api_key_env_var_name(&self) -> &str {
        &self.api_key_env
    }

    fn build_request(
        &self,
        client: &Client,
        sys_prompt: &str,
        user_prompt: &str,
        json_schema: &Schema,
    ) -> RequestBuilder {
        // Request body
        // https://docs.anthropic.com/en/api/messages
        let request_body = json!({
            "model": self.model,
            "system": sys_prompt,
            "messages": [
                {
                    "role": "user",
                    "content": user_prompt
                }
            ],
            "max_tokens": 4096,
            "tools": [{
                "name": TOOL_NAME,
                "description": "Propose an structured response to the end user",
                "input_schema": json_schema,
            }],
            "tool_choice": {
                "type": "tool",
                "name": TOOL_NAME,
                "disable_parallel_tool_use": true
            }
        });

        tracing::trace!("Request:\n{request_body:#}");

        // Messages url
        let url = format!("{}/messages", self.url);

        // Request
        client
            .post(url)
            .header("anthropic-version", "2023-06-01")
            .json(&request_body)
    }
}

impl AiProvider for AnthropicModelConfig {
    async fn parse_response<T>(&self, res: Response) -> Result<T>
    where
        T: DeserializeOwned + JsonSchema + Debug,
    {
        // Parse successful response
        let res: Json = res.json().await.wrap_err("Anthropic response not a json")?;
        tracing::trace!("Response:\n{res:#}");
        let mut res: AnthropicResponse<T> =
            serde_json::from_value(res).wrap_err("Couldn't parse Anthropic response")?;

        // Validate the response content
        if res.stop_reason != "end_turn" && res.stop_reason != "tool_use" {
            tracing::error!("OpenAI response got an invalid stop reason: {}", res.stop_reason);
            return Err(UserFacingError::AiRequestFailed(format!(
                "couldn't generate a valid response: {}",
                res.stop_reason
            ))
            .into());
        }

        if res.content.is_empty() {
            tracing::error!("Response got no content: {res:?}");
            return Err(UserFacingError::AiRequestFailed(String::from("received response with no content")).into());
        } else if res.content.len() > 1 {
            tracing::warn!("Response got {} content blocks", res.content.len());
        }

        let block = res.content.remove(0);
        if block.r#type != "tool_use" {
            tracing::error!("Anthropic response got an invalid content type: {}", block.r#type);
            return Err(UserFacingError::AiRequestFailed(format!("unexpected response type: {}", block.r#type)).into());
        }

        if block.name != TOOL_NAME {
            tracing::error!("Anthropic response got an invalid tool name: {}", block.name);
            return Err(UserFacingError::AiRequestFailed(format!("received invalid tool name: {}", block.name)).into());
        }

        Ok(block.input)
    }
}

#[derive(Debug, Deserialize)]
struct AnthropicResponse<T> {
    content: Vec<ContentBlock<T>>,
    stop_reason: String,
}

#[derive(Debug, Deserialize)]
struct ContentBlock<T> {
    r#type: String,
    name: String,
    input: T,
}

/// The client every AI task in the TUI goes through
pub struct AiClient {
    inner: Client,
    primary: AnthropicModelConfig,
    api_key: String,
}

impl AiClient {
    /// Generate some command suggestions based on the given prompt
    pub async fn generate_command_suggestions(
        &self,
        sys_prompt: &str,
        user_prompt: &str,
    ) -> Result<CommandSuggestions> {
        self.generate_content(sys_prompt, user_prompt).await
    }

    /// The inner logic to generate content from a prompt with an AI provider.
    async fn generate_content<T>(&self, sys_prompt: &str, user_prompt: &str) -> Result<T>
    where
        T: DeserializeOwned + JsonSchema + Debug,
    {
        // Generate the json schema from the expected type
        let json_schema = schema_for!(T);

        // Build and send the request
        let (header, value) = self.primary.auth_header(self.api_key.clone());
        let res = self
            .primary
            .build_request(&self.inner, sys_prompt, user_prompt, &json_schema)
            .header(header, value)
            .send()
            .await
            .wrap_err("Couldn't send the request")?;

        self.primary.parse_response(res).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    #[ignore] // Real API calls require valid api keys
    async fn test_anthropic_api() -> Result<()> {
        let config = AnthropicModelConfig {
            model: "claude-sonnet-4-0".into(),
            url: "https://api.anthropic.com/v1".into(),
            api_key_env: "ANTHROPIC_API_KEY".into(),
        };
        let client = AiClient {
            inner: Client::new(),
            primary: config,
            api_key: std::env::var("ANTHROPIC_API_KEY").unwrap_or_default(),
        };
        let res = client
            .generate_command_suggestions(
                "you're a cli expert, that will proide command suggestions based on what the user want to do",
                "undo last n amount of commits",
            )
            .await?;
        tracing::info!("Suggestions:");
        for command in res.suggestions {
            tracing::info!("  # {}", command.description);
            tracing::info!("  {}", command.command);
        }
        Ok(())
    }
}
