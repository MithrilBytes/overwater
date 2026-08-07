import Foundation

struct PenPal {
    func reply(history: [Message]) async throws -> String {
        let request = ChatRequest.builder()
            .model("gpt-4o-mini")
            .messages(history)
            .maxTokens(600)
            .system("You are a friendly pen pal from Lisbon. Write back in a chatty paragraph and ask one question.")
            .build()
        return try await client.send(request).text
    }
}
