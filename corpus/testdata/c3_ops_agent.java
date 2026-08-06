import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.core.JsonValue;
import com.anthropic.models.messages.Message;
import com.anthropic.models.messages.MessageCreateParams;
import com.anthropic.models.messages.Tool;
import java.util.Map;

public class OpsRunner {
    private final AnthropicClient client = AnthropicOkHttpClient.fromEnv();

    public Message nextTurn(String alert) {
        Tool restart = Tool.builder()
                .name("restart_service")
                .description("Restart a service by name and report the new status.")
                .inputSchema(Tool.InputSchema.builder()
                        .properties(JsonValue.from(Map.of("service", Map.of("type", "string"))))
                        .build())
                .build();
        MessageCreateParams params = MessageCreateParams.builder()
                .model("claude-sonnet-5")
                .maxTokens(2048L)
                .addTool(restart)
                .addUserMessage(alert)
                .build();
        return client.messages().create(params);
    }
}
