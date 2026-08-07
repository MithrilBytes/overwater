package com.acme.forum;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class PostGate {
    private final AnthropicClient client;

    public PostGate(AnthropicClient client) {
        this.client = client;
    }

    public boolean allowed(String post) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-haiku-4-5")
                .maxTokens(4L)
                .temperature(0.0)
                .system("You enforce the forum rules. Block harassment, spam, and personal data. Answer allow or block.")
                .addUserMessage(post)
                .build();
        return client.messages().create(params).content().get(0).text().get().text().trim().equals("allow");
    }
}
