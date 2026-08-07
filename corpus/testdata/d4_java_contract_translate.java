package com.acme.legal;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class ContractTranslator {
    private final AnthropicClient client;

    public ContractTranslator(AnthropicClient client) {
        this.client = client;
    }

    public String translate(String clause, String target) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-5")
                .maxTokens(3000L)
                .temperature(0.0)
                .system("Translate the clause into the target language. Keep defined terms capitalized and numbering intact.")
                .addUserMessage(target + "\n" + clause)
                .build();
        return client.messages().create(params).content().get(0).text().get().text();
    }
}
