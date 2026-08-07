using Azure.AI.OpenAI;
using OpenAI.Chat;

public class HelpdeskBot
{
    private readonly ChatClient _chat;

    public HelpdeskBot(AzureOpenAIClient client)
    {
        _chat = client.GetChatClient("gpt-4.1");
    }

    public string Respond(List<ChatMessage> history)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 700, Temperature = 0.6f };
        history.Insert(0, new SystemChatMessage(
            "You are the internal IT helpdesk assistant. Keep the tone friendly and ask for the laptop tag when it is missing."));
        return _chat.CompleteChat(history, options).Value.Content[0].Text;
    }
}
