using OpenAI.Chat;

public class FaqRanker
{
    private readonly ChatClient _chat = new("gpt-4o-mini", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string Rank(string question, string faqList)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 100, Temperature = 0f };
        var messages = new ChatMessage[]
        {
            new SystemChatMessage("Order the FAQ entries by how closely they match the question. Return the entry ids, best first."),
            new UserChatMessage(question + "\n" + faqList),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text;
    }
}
