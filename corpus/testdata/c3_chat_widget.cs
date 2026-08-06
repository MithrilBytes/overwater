using System;
using System.Net.Http;
using System.Net.Http.Json;
using System.Threading.Tasks;

public class ChatWidget
{
    private static readonly HttpClient Http = new HttpClient();

    public static async Task<HttpResponseMessage> StreamChatReplyAsync(object[] history)
    {
        var payload = new
        {
            model = "grok-4",
            stream = true,
            messages = history,
        };
        var request = new HttpRequestMessage(HttpMethod.Post, "https://api.x.ai/v1/chat/completions")
        {
            Content = JsonContent.Create(payload),
        };
        request.Headers.Add("Authorization", "Bearer " + Environment.GetEnvironmentVariable("XAI_API_KEY"));
        return await Http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead);
    }
}
