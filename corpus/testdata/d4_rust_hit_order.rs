use async_openai::types::CreateChatCompletionRequestArgs;
use async_openai::Client;

pub async fn order_hits(query: &str, hits: &str) -> Result<String, Box<dyn std::error::Error>> {
    let client = Client::new();
    let request = CreateChatCompletionRequestArgs::default()
        .model("gpt-4o-mini")
        .max_tokens(90u32)
        .temperature(0.0)
        .messages(system_and_user(
            "Rank the numbered search hits by relevance to the query and return the numbers in order.",
            &format!("query: {query}\n{hits}"),
        ))
        .build()?;
    let response = client.chat().create(request).await?;
    Ok(response.choices[0].message.content.clone().unwrap_or_default())
}
