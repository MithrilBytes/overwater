package com.acme.schema;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class MigrationWriter {
    private final AnthropicClient client;

    public MigrationWriter(AnthropicClient client) {
        this.client = client;
    }

    public String write(String currentDdl, String change) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-5")
                .maxTokens(2000L)
                .temperature(0.1)
                .system("Emit the Flyway migration SQL for the requested schema change. SQL only.")
                .addUserMessage(currentDdl + "\n" + change)
                .build();
        return client.messages().create(params).content().get(0).text().get().text();
    }
}
