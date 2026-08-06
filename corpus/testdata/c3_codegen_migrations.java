import com.openai.client.OpenAIClient;
import com.openai.client.okhttp.OpenAIOkHttpClient;
import com.openai.models.chat.completions.ChatCompletion;
import com.openai.models.chat.completions.ChatCompletionCreateParams;

public class MigrationBot {
    private final OpenAIClient client = OpenAIOkHttpClient.fromEnv();

    public String codegenSqlMigration(String changeRequest) {
        ChatCompletionCreateParams params = ChatCompletionCreateParams.builder()
                .model("o3-mini")
                .addSystemMessage("You are the codegen backend. Write a Flyway SQL migration for the requested schema change. Output SQL only.")
                .addUserMessage(changeRequest)
                .build();
        ChatCompletion completion = client.chat().completions().create(params);
        return completion.choices().get(0).message().content().orElse("");
    }
}
