using OpenAI.Chat;

public class MeetingNotes
{
    private readonly ChatClient _chat = new("gpt-4.1-mini", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string Write(string transcript)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 800 };
        var messages = new ChatMessage[]
        {
            new SystemChatMessage("Write the meeting notes: decisions, owners, and dates, in prose. Skip anything that was not agreed."),
            new UserChatMessage(transcript),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text;
    }
}
