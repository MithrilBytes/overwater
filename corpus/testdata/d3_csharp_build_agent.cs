using OpenAI.Chat;

public class BuildAgent
{
    private readonly ChatClient _chat = new("gpt-5-mini", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    // Called in a loop until the model returns no tool calls.
    public ChatCompletion Step(List<ChatMessage> scratchpad, IList<ChatTool> tools)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 2500 };
        foreach (var t in tools)
        {
            options.Tools.Add(t);
        }
        return _chat.CompleteChat(scratchpad, options).Value;
    }
}
