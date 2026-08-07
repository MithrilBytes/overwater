using OpenAI.Chat;

public class FormReader
{
    private readonly ChatClient _chat = new("gpt-4.1-nano", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string ReadFields(string form)
    {
        var options = new ChatCompletionOptions { MaxOutputTokenCount = 300, Temperature = 0f };
        var messages = new ChatMessage[]
        {
            new SystemChatMessage("Return JSON with applicant_name, ssn_last4, annual_income, and employer, copied from the form."),
            new UserChatMessage(form),
        };
        return _chat.CompleteChat(messages, options).Value.Content[0].Text;
    }
}
