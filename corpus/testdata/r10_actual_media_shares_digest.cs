// From Actual-Chat/actual-chat, src/dotnet/Chat.ML/ChatDigestSummarizer.cs, with
// the keyed Semantic Kernel registration from
// src/dotnet/Chat.Service/Module/ChatServiceModule.cs and the model default from
// Module/ChatSettings.cs, which is where the repository keeps it. Summarize, whose
// prompt is a file outside the tree, is dropped; SummarizeMediaShares builds its
// prompt inline and is kept. The platform wide model fallback inside
// AddKeyedOpenAI is dropped so one model string is left.

using System.Net;
using ActualChat.AI;
using ActualLab.IO;
using Microsoft.SemanticKernel;
using Microsoft.SemanticKernel.ChatCompletion;

namespace ActualChat.Chat.ML;

public sealed class ChatSettings
{
    public bool IsSummarizationEnabled { get; set; }
    public SummarizationSettings Summarization { get; set; } = new ();
}

public class SummarizationSettings
{
    public string OpenAIModel { get; set; } = "gpt-4.1";
    public int MinConversationWords { get; set; } = 1200;
    public int MinConversationEntries { get; set; } = 10;
    public FilePath SummarizeChatDigestPromptFile { get; set; } = "summarize-chat-digest.md";
    public TimeSpan HttpTimeout { get; set; } = TimeSpan.FromMinutes(5);
}

public sealed class ChatServiceModule(IServiceProvider moduleServices)
    : HostModule<ChatSettings>(moduleServices), IServerModule
{
    private CoreServerSettings CoreServerSettings => field ??= Cfg.Settings<CoreServerSettings>(nameof(CoreSettings));

    protected override void InjectServices(IServiceCollection services)
    {
        if (Settings.IsSummarizationEnabled) {
            AddKeyedOpenAI(services,
                ConversationSummarizer.ServiceKey,
                Settings.Summarization.OpenAIModel,
                Settings.Summarization.HttpTimeout);
            services.AddSingleton<IChatDigestSummarizer, ChatDigestSummarizer>(
                c => new ChatDigestSummarizer(
                    new ChatDigestSummarizer.Options {
                        PromptFile = c.GetRequiredService<CoreServerSettings>().PromptsDir
                            | Settings.Summarization.SummarizeChatDigestPromptFile,
                    },
                    c));
        }
        else {
            services.AddSingleton<IChatDigestSummarizer, ChatDigestSummarizerStub>();
        }
    }

    private void AddKeyedOpenAI(
        IServiceCollection services,
        string serviceKey,
        string openAIModel,
        TimeSpan? httpClientTimeout = null)
    {
        var loggingHandlerOptions = new OpenAIRateLimitsLoggingHandler.Options(false);
        var httpClient = new HttpClient(new OpenAIRateLimitsLoggingHandler(loggingHandlerOptions) {
            InnerHandler = new HttpClientHandler {
                Proxy = !CoreServerSettings.OpenAIProxy.IsNullOrEmpty()
                    ? new WebProxy(CoreServerSettings.OpenAIProxy)
                    : null,
                UseProxy = !CoreServerSettings.OpenAIProxy.IsNullOrEmpty(),
            },
        }) {
            Timeout = httpClientTimeout ?? TimeSpan.FromSeconds(100),
        };
        services.AddKeyedSingleton(serviceKey, httpClient); // for disposal
        var openAIKey = CoreServerSettings.OpenAIKey;

        // unlimited
        var unlimitedServiceKey = serviceKey + "_Unlimited";
        services.AddKernel().AddOpenAIChatCompletion(
            openAIModel, openAIKey, serviceId: unlimitedServiceKey, httpClient: httpClient);

        // rate-limited
        var rateLimitedKey = serviceKey + "_RateLimited";
        services.AddKeyedSingleton<IChatCompletionService>(rateLimitedKey,
            (c, _) => {
                var chatCompletion = c.GetRequiredKeyedService<IChatCompletionService>(unlimitedServiceKey);
                var rateLimiter = new RedisTokenBucketRateLimiter(
                    c.GetRequiredService<RedisDb<ChatDbContext>>(),
                    "",
                    new TokenBucketBudget(2_000_000, TimeSpan.FromSeconds(60)));
                return chatCompletion.WrapWithRateLimiter(rateLimiter, $"rate_limit:openai:{serviceKey}");
            });

        // for serviceKey
        services.AddKeyedSingleton<IChatCompletionService>(serviceKey,
            (c, _) => c.GetRequiredKeyedService<IChatCompletionService>(rateLimitedKey));
    }
}

public interface IChatDigestSummarizer
{
    Task<string?> SummarizeMediaShares(
        IReadOnlyCollection<ChatEntry> mediaEntries,
        Language language,
        CancellationToken cancellationToken);
}

public class ChatDigestSummarizer(ChatDigestSummarizer.Options settings, IServiceProvider services) : IChatDigestSummarizer
{
    public class Options
    {
        public FilePath PromptFile { get; set; } = "";
    }

    public const string ServiceKey = ConversationSummarizer.ServiceKey;

    private Options Settings { get; } = settings;
    private Kernel Kernel => field ??= services.GetRequiredService<Kernel>();
    private IChatCompletionService ChatCompletionService => field ??= Kernel.GetRequiredService<IChatCompletionService>(ServiceKey);
    private IPromptHelpers PromptHelpers => field ??= services.GetRequiredService<IPromptHelpers>();
    private IAuthorNameRetriever AuthorNameRetriever => field ??= services.GetRequiredService<IAuthorNameRetriever>();
    private ILogger Log => field ??= services.LogFor(GetType());

    public async Task<string?> SummarizeMediaShares(
        IReadOnlyCollection<ChatEntry> mediaEntries,
        Language language,
        CancellationToken cancellationToken)
    {
        if (mediaEntries.Count == 0)
            return null;

        var shares = await BuildShares(mediaEntries).ConfigureAwait(false);
        if (shares.Count == 0)
            return null;

        var sharesText = string.Join("\n", shares.Select(FormatShareLine));
        var prompt = $"""
            You are summarizing media-only activity in a chat (no text messages were sent).

            Below is who shared what during the period:
            {sharesText}

            Write a one-line summary in {language.Title} describing what was shared and by whom.
            Use the authors' names verbatim. Wrap the result in <summary>...</summary> tags.
            """;
        try {
            var response = await Ask(prompt, cancellationToken).ConfigureAwait(false);
            var summary = PromptHelpers.GetXmlTagValue(response, "summary").Trim();
            return summary.IsNullOrEmpty() ? null : summary;
        }
        catch (Exception ex) {
            Log.LogError(ex, "Media-shares summarization error");
            return null;
        }
    }

    private async Task<List<Share>> BuildShares(IReadOnlyCollection<ChatEntry> mediaEntries)
    {
        var perAuthor = new Dictionary<AuthorId, Share>();
        foreach (var entry in mediaEntries) {
            if (!perAuthor.TryGetValue(entry.AuthorId, out var share))
                share = new Share(await AuthorNameRetriever.GetAuthorName(entry.AuthorId).ConfigureAwait(false));
            foreach (var a in entry.Attachments)
                if (a.IsSupportedImage())
                    share.Images++;
                else if (a.IsSupportedVideo())
                    share.Videos++;
                else
                    share.FileNames.Add(a.Media.FileName);
            perAuthor[entry.AuthorId] = share;
        }
        return perAuthor.Values.Where(s => s.Images + s.Videos + s.FileNames.Count > 0).ToList();
    }

    private static string FormatShareLine(Share s)
    {
        var parts = new List<string>(3);
        if (s.Images > 0)
            parts.Add(s.Images == 1 ? "an image" : $"{s.Images} images");
        if (s.Videos > 0)
            parts.Add(s.Videos == 1 ? "a video" : $"{s.Videos} videos");
        if (s.FileNames.Count == 1)
            parts.Add(s.FileNames[0]);
        else if (s.FileNames.Count > 1)
            parts.Add($"{s.FileNames.Count} files");
        return $"- {s.AuthorName}: {string.Join(", ", parts)}";
    }

    private async Task<string> Ask(string prompt, CancellationToken cancellationToken)
    {
        var response = await ChatCompletionService
            .GetChatMessageContentAsync(prompt, null, Kernel, cancellationToken)
            .ConfigureAwait(false);
        return response.Content ?? "";
    }

    private sealed class Share(string authorName)
    {
        public string AuthorName { get; } = authorName;
        public int Images { get; set; }
        public int Videos { get; set; }
        public List<string> FileNames { get; } = new();
    }
}

public class ChatDigestSummarizerStub : IChatDigestSummarizer
{
    public Task<string?> SummarizeMediaShares(IReadOnlyCollection<ChatEntry> mediaEntries, Language language, CancellationToken cancellationToken)
        => Task.FromResult<string?>(null);
}
