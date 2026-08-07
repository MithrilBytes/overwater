package com.acme.search;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class DocRanker {
    private final AnthropicClient client;

    public DocRanker(AnthropicClient client) {
        this.client = client;
    }

    public String rank(String query, String docs) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-haiku-4-5")
                .maxTokens(120L)
                .temperature(0.0)
                .system("Put the numbered documents in order of relevance to the query, best first. Return the numbers only.")
                .addUserMessage(query + "\n" + docs)
                .build();
        return client.messages().create(params).content().get(0).text().get().text();
    }
}
