using System;
using System.Net.Http;
using System.Net.Http.Json;
using System.Threading.Tasks;

public class VoicemailInbox
{
    private static readonly HttpClient Http = new HttpClient();

    public static async Task<string> TranscribeVoicemailAsync(string audioBase64)
    {
        var prompt = new { text = "Transcribe the voicemail exactly as spoken. No commentary." };
        var audio = new { inline_data = new { mime_type = "audio/mp3", data = audioBase64 } };
        var payload = new { contents = new[] { new { parts = new object[] { prompt, audio } } } };
        var url = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"
            + "?key=" + Environment.GetEnvironmentVariable("GEMINI_API_KEY");
        var response = await Http.PostAsJsonAsync(url, payload);
        return await response.Content.ReadAsStringAsync();
    }
}
