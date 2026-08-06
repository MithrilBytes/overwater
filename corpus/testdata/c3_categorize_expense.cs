using System.Collections.Generic;
using System.Threading.Tasks;
using Anthropic.SDK;
using Anthropic.SDK.Messaging;

public class ExpenseCoder
{
    public static async Task<string> CategorizeExpenseAsync(AnthropicClient client, string description)
    {
        var parameters = new MessageParameters
        {
            Model = "claude-haiku-4-5",
            MaxTokens = 20,
            System = new List<SystemMessage>
            {
                new SystemMessage("Assign the expense to one category: travel, meals, software, office, or other."),
            },
            Messages = new List<Message> { new Message(RoleType.User, description) },
        };
        var response = await client.Messages.GetClaudeMessageAsync(parameters);
        return response.Message.ToString().Trim().ToLowerInvariant();
    }
}
