package com.acme.travel;

import com.anthropic.client.AnthropicClient;
import com.anthropic.models.messages.Message;
import com.anthropic.models.messages.MessageCreateParams;
import com.anthropic.models.messages.Tool;
import java.util.List;

public class TravelAgent {
    private final AnthropicClient client;

    public TravelAgent(AnthropicClient client) {
        this.client = client;
    }

    public Message step(List<Tool> tools, String userGoal) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-5")
                .maxTokens(3000L)
                .system("You are a travel agent. Search fares, hold a seat, and book only after the traveler confirms.")
                .tools(tools)
                .addUserMessage(userGoal)
                .build();
        return client.messages().create(params);
    }
}
