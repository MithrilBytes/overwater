package com.acme.research;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class EarningsBrief {
    private final AnthropicClient client;

    public EarningsBrief(AnthropicClient client) {
        this.client = client;
    }

    public String brief(String callText) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-5")
                .maxTokens(1500L)
                .system("Write a two paragraph brief on this earnings call for a portfolio manager. Keep the guidance numbers.")
                .addUserMessage(callText)
                .build();
        return client.messages().create(params).content().get(0).text().get().text();
    }
}
