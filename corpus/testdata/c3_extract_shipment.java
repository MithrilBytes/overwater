import com.openai.client.OpenAIClient;
import com.openai.client.okhttp.OpenAIOkHttpClient;
import com.openai.models.chat.completions.ChatCompletion;
import com.openai.models.chat.completions.ChatCompletionCreateParams;

public class ShipmentIntake {
    private final OpenAIClient client = OpenAIOkHttpClient.fromEnv();

    public String extractShipmentDetails(String billOfLading) {
        ChatCompletionCreateParams params = ChatCompletionCreateParams.builder()
                .model("gpt-5.1")
                .addSystemMessage("Parse the bill of lading. Return ports, container ids, and eta as JSON.")
                .addUserMessage(billOfLading)
                .build();
        ChatCompletion completion = client.chat().completions().create(params);
        return completion.choices().get(0).message().content().orElse("");
    }
}
