using OpenAI.Chat;

public class UiDiffReviewer
{
    private readonly ChatClient _chat = new("gpt-4.1", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string Review(Uri beforeShot, Uri afterShot)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 500 };
        var messages = new ChatMessage[]
        {
            new UserChatMessage(
                ChatMessageContentPart.CreateTextPart("What changed visually between these two screenshots?"),
                ChatMessageContentPart.CreateImagePart(beforeShot),
                ChatMessageContentPart.CreateImagePart(afterShot)),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text;
    }
}
