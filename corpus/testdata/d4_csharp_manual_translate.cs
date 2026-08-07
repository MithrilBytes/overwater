using OpenAI.Chat;

public class ManualTranslator
{
    private readonly ChatClient _chat = new("gpt-4.1-mini", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string Translate(string section, string target)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 2000, Temperature = 0f };
        var messages = new ChatMessage[]
        {
            new SystemChatMessage("Translate the service manual section into the target language. Keep part numbers and units as written."),
            new UserChatMessage(target + "\n" + section),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text;
    }
}
