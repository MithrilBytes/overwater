import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.core.http.StreamResponse;
import com.anthropic.models.messages.MessageCreateParams;
import com.anthropic.models.messages.RawMessageStreamEvent;

public class ChatRelay {
    private final AnthropicClient client = AnthropicOkHttpClient.fromEnv();

    public void chatTurn(String userText) {
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-opus-5")
                .maxTokens(900L)
                .system("You are the concierge assistant. Keep the conversation warm and brief.")
                .addUserMessage(userText)
                .build();
        try (StreamResponse<RawMessageStreamEvent> stream = client.messages().createStreaming(params)) {
            stream.stream().forEach(event -> System.out.print(event));
        }
    }
}
