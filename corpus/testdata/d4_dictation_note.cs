using OpenAI.Audio;

public class Dictation
{
    private readonly AudioClient _audio = new("gpt-4o-mini", Environment.GetEnvironmentVariable("OPENAI_API_KEY"));

    public string ToText(string path)
    {
        var options = new AudioTranscriptionOptions { Language = "en", ResponseFormat = AudioTranscriptionFormat.Text };
        return _audio.TranscribeAudio(path, options).Value.Text;
    }
}
