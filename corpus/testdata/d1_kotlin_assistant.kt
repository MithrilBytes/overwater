package com.acme.assistant

import com.anthropic.client.AnthropicClient
import com.anthropic.models.messages.MessageCreateParams

class AssistantTurn(private val client: AnthropicClient) {
    fun reply(userText: String): String {
        val params = MessageCreateParams.builder()
            .model("claude-sonnet-4-5")
            .maxTokens(900L)
            .system("You are the Acme desktop assistant. Talk the user through their question one step at a time.")
            .addUserMessage(userText)
            .build()
        return client.messages().create(params).content().first().text().get().text()
    }
}
