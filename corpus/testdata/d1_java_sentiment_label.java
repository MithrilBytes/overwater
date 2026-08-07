package com.acme.reviews;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class ReviewSentiment {
    private final AnthropicClient client;

    public ReviewSentiment(AnthropicClient client) {
        this.client = client;
    }

    public String label(String review) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-haiku-4-5")
                .maxTokens(6L)
                .temperature(0.0)
                .system("Label the review sentiment as positive, neutral, or negative. One word only.")
                .addUserMessage(review)
                .build();
        return client.messages().create(params).content().get(0).text().get().text().trim();
    }
}
