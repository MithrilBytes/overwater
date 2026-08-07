using OpenAI.Chat;

public class StubGenerator
{
    private readonly ChatClient _chat = new("gpt-4.1", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string FromOpenApi(string spec)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 3000, Temperature = 0.1f };
        var messages = new ChatMessage[]
        {
            new SystemChatMessage("Generate the C# client class for this OpenAPI path. Emit compilable code only."),
            new UserChatMessage(spec),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text;
    }
}
