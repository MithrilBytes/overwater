using OpenAI.Chat;

public class AbuseGate
{
    private readonly ChatClient _chat = new("gpt-4.1-nano", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public bool Blocked(string message)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 3, Temperature = 0f };
        var messages = new ChatMessage[]
        {
            new SystemChatMessage("Flag abusive messages aimed at support agents. Answer block or allow."),
            new UserChatMessage(message),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text.Trim() == "block";
    }
}
