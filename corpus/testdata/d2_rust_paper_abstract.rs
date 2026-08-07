use async_openai::types::CreateChatCompletionRequestArgs;
use async_openai::Client;

pub async fn plain_abstract(paper: &str) -> Result<String, Box<dyn std::error::Error>> {
    let client = Client::new();
    let request = CreateChatCompletionRequestArgs::default()
        .model("gpt-4.1")
        .max_tokens(700u32)
        .messages(system_and_user(
            "Rewrite the paper's findings as a plain language summary for a newsletter. Three short paragraphs.",
            paper,
        ))
        .build()?;
    let response = client.chat().create(request).await?;
    Ok(response.choices[0].message.content.clone().unwrap_or_default())
}
