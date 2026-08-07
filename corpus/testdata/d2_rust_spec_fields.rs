use async_openai::types::CreateChatCompletionRequestArgs;
use async_openai::Client;

pub async fn read_specs(listing: &str) -> Result<String, Box<dyn std::error::Error>> {
    let client = Client::new();
    let request = CreateChatCompletionRequestArgs::default()
        .model("gpt-4o-mini")
        .max_tokens(350u32)
        .temperature(0.0)
        .messages(system_and_user(
            "Return JSON with cpu, ram_gb, storage_gb, and screen_inches copied from the product listing.",
            listing,
        ))
        .build()?;
    let response = client.chat().create(request).await?;
    Ok(response.choices[0].message.content.clone().unwrap_or_default())
}
