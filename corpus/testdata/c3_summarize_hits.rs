use async_openai::types::{
    ChatCompletionRequestSystemMessageArgs, ChatCompletionRequestUserMessageArgs,
    CreateChatCompletionRequestArgs,
};
use async_openai::Client;

// Despite the name, this call reorders retrieved passages for a query.
pub async fn summarize_hits(query: &str, hits: &str) -> Result<String, Box<dyn std::error::Error>> {
    let client = Client::new();
    let system = ChatCompletionRequestSystemMessageArgs::default()
        .content("Rerank the numbered passages by relevance to the query. Return passage numbers only, best first.")
        .build()?;
    let user = ChatCompletionRequestUserMessageArgs::default()
        .content(format!("query: {query}\n{hits}"))
        .build()?;
    let request = CreateChatCompletionRequestArgs::default()
        .model("gpt-4o-mini")
        .messages(vec![system.into(), user.into()])
        .build()?;
    let response = client.chat().create(request).await?;
    Ok(response.choices[0].message.content.clone().unwrap_or_default())
}
