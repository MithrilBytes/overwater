package com.acme.kyc;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.MessageCreateParams;

public class IdCardReader {
    private final AnthropicClient client;

    public IdCardReader(AnthropicClient client) {
        this.client = client;
    }

    public String read(String imageBase64) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-4-5")
                .maxTokens(400L)
                .system("You read photographs of identity documents and report what is printed on them.")
                .addUserMessageOfImage(imageBase64, "image/jpeg")
                .build();
        return client.messages().create(params).content().get(0).text().get().text();
    }
}
