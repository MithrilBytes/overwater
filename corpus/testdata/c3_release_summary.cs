using System.Threading.Tasks;
using Azure;
using Azure.AI.OpenAI;

public class ReleaseNotes
{
    public static async Task<string> SummarizeReleaseAsync(OpenAIClient client, string commits)
    {
        var options = new ChatCompletionsOptions
        {
            DeploymentName = "gpt-4o",
            MaxTokens = 400,
            Messages =
            {
                new ChatRequestSystemMessage("Summarize the merged commits into short release notes for customers."),
                new ChatRequestUserMessage(commits),
            },
        };
        Response<ChatCompletions> response = await client.GetChatCompletionsAsync(options);
        return response.Value.Choices[0].Message.Content;
    }
}
