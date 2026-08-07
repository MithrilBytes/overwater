package com.acme.logistics;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class BolReader {
    private final AnthropicClient client;

    public BolReader(AnthropicClient client) {
        this.client = client;
    }

    public String read(String billOfLading) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-4-5")
                .maxTokens(400L)
                .temperature(0.0)
                .system("Copy the shipper, consignee, container number, and weight off the bill of lading into JSON.")
                .addUserMessage(billOfLading)
                .build();
        return client.messages().create(params).content().get(0).text().get().text();
    }
}
