use async_openai::types::CreateChatCompletionRequestArgs;
use async_openai::Client;

pub async fn intent_of(utterance: &str) -> Result<String, Box<dyn std::error::Error>> {
    let client = Client::new();
    let request = CreateChatCompletionRequestArgs::default()
        .model("gpt-4o-mini")
        .max_tokens(6u32)
        .temperature(0.0)
        .messages(system_and_user(
            "Return one intent from: book_table, cancel, hours, other. Nothing else.",
            utterance,
        ))
        .build()?;
    let response = client.chat().create(request).await?;
    Ok(response.choices[0].message.content.clone().unwrap_or_default())
}
