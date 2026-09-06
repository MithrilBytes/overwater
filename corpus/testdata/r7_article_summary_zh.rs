// From zhanglun/lettura: apps/desktop/src-tauri/src/ai/summary.rs, with
// the OpenAILLM adapter from apps/desktop/src-tauri/src/ai/llm.rs and the
// model default from apps/desktop/src-tauri/src/ai/config.rs, which is
// where the repository keeps it. The prompt is Chinese in the original
// and is kept that way. The tool-calling path and the mock provider are
// dropped.

use async_openai::config::OpenAIConfig;
use async_openai::types::chat::{
  ChatCompletionRequestMessage, ChatCompletionRequestSystemMessage,
  ChatCompletionRequestSystemMessageContent, ChatCompletionRequestUserMessage,
  ChatCompletionRequestUserMessageContent, CreateChatCompletionRequestArgs,
};
use async_openai::Client;
use async_trait::async_trait;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AiConfig {
  #[serde(default)]
  pub api_key: String,
  #[serde(default = "default_model")]
  pub model: String,
  #[serde(default = "default_base_url")]
  pub base_url: String,
}

fn default_model() -> String {
  "gpt-4o-mini".to_string()
}

fn default_base_url() -> String {
  "https://api.openai.com/v1".to_string()
}

impl Default for AiConfig {
  fn default() -> Self {
    Self {
      api_key: String::new(),
      model: default_model(),
      base_url: default_base_url(),
    }
  }
}

#[async_trait]
pub trait LLMProvider: Send + Sync {
  async fn complete(&self, prompt: &str, system: &str) -> Result<String, String>;
}

pub struct OpenAILLM {
  client: Client<OpenAIConfig>,
  model: String,
}

impl OpenAILLM {
  pub fn new(api_key: &str, base_url: &str, model: String) -> Self {
    let config = OpenAIConfig::new()
      .with_api_key(api_key)
      .with_api_base(base_url);
    Self {
      client: Client::with_config(config),
      model,
    }
  }

  pub fn from_config(config: &AiConfig) -> Self {
    Self::new(&config.api_key, &config.base_url, config.model.clone())
  }

  /// Single-shot completion (used by summary/why_it_matters/etc.).
  pub async fn complete(&self, prompt: &str, system: &str) -> Result<String, String> {
    let request = CreateChatCompletionRequestArgs::default()
      .model(&self.model)
      .max_tokens(512u32)
      .messages(vec![
        ChatCompletionRequestMessage::System(ChatCompletionRequestSystemMessage {
          content: ChatCompletionRequestSystemMessageContent::Text(system.to_string()),
          name: None,
        }),
        ChatCompletionRequestMessage::User(ChatCompletionRequestUserMessage {
          content: ChatCompletionRequestUserMessageContent::Text(prompt.to_string()),
          name: None,
        }),
      ])
      .build()
      .map_err(|e| format!("LLM request build failed: {}", e))?;

    let response = self
      .client
      .chat()
      .create(request)
      .await
      .map_err(|e| format!("LLM API call failed: {}", e))?;

    response
      .choices
      .first()
      .and_then(|c| c.message.content.clone())
      .ok_or_else(|| "No content in LLM response".to_string())
  }
}

#[async_trait]
impl LLMProvider for OpenAILLM {
  /// Delegate single-shot completion to the helper above.
  async fn complete(&self, prompt: &str, system: &str) -> Result<String, String> {
    OpenAILLM::complete(self, prompt, system).await
  }
}

pub async fn generate_summary(
  llm: &dyn LLMProvider,
  title: &str,
  content_truncated: &str,
) -> Result<String, String> {
  let prompt = format!(
    r#"你是一位精确的内容分析师。请用一句话总结以下文章。

规则：
- 中文不超过80字，英文不超过50词
- 直接陈述要点，不要用"本文讨论了..."之类的开头
- 使用客观、事实性的语言，不要使用夸张词
- 如果文章涉及某个产品/工具，请包含其名称
- 如果文章包含量化结论，请包含关键数字

文章标题：{}
文章内容：{}

请直接输出总结内容，不要加前缀或引号。"#,
    title, content_truncated
  );

  let result = llm
    .complete(&prompt, "你是一位精确的内容分析师。请用中文输出。")
    .await?;
  let trimmed = result.trim().to_string();

  if validate_summary(&trimmed) {
    Ok(trimmed)
  } else {
    Ok(trimmed)
  }
}

pub fn validate_summary(text: &str) -> bool {
  let word_count = text.split_whitespace().count();
  word_count > 0 && word_count <= 60
}
