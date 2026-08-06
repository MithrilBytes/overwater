use async_openai::types::CreateEmbeddingRequestArgs;
use async_openai::Client;

pub async fn embed_articles(texts: Vec<String>) -> Result<Vec<Vec<f32>>, Box<dyn std::error::Error>> {
    let client = Client::new();
    let request = CreateEmbeddingRequestArgs::default()
        .model("text-embedding-3-small")
        .input(texts)
        .build()?;
    let response = client.embeddings().create(request).await?;
    Ok(response.data.into_iter().map(|d| d.embedding).collect())
}
